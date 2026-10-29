"""Device operations: reachability checks and configuration backups.

Both only read from switches, so a lost lease fails the item instead of risking a
duplicate change, and the next scheduled run tries again.
"""

import asyncio
import base64
import hashlib
import json
import logging
import os
import re
import time
from contextlib import suppress
from types import SimpleNamespace

from bind_plane.services.command_profiles import output_matches_any
from bind_plane.worker.sessions import (
    NetmikoSwitchSession,
    SwitchConnectionTimeoutError,
    SwitchSessionError,
)
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from netmiko.exceptions import NetmikoTimeoutException, ReadTimeout

logger = logging.getLogger("bind_plane.worker.devices")

# Must match the API's configBackupAAD: it binds ciphertexts to the configuration purpose.
CONFIG_AAD = b"bind-plane config backup v1"
VENDOR_DEVICE_TYPES = {
    "huawei": "huawei",
    "h3c": "hp_comware",
    "ruijie": "ruijie_os",
    "cisco": "cisco_ios",
}
DEFAULT_PORTS = {"telnet": 23, "ssh": 22}
# The legacy profile default; switches with vendor "other" keep exactly that behaviour.
LEGACY_DEVICE_TYPE = "hp_comware_telnet"
VOLATILE_LINES = (
    r"^! Last configuration change at",
    r"^! NVRAM config last updated at",
    r"^ntp clock-period",
)
MAX_CONFIG_BYTES = 20 * 1024 * 1024
REACHABILITY_TIMEOUT = 3.0


def credential_key() -> bytes:
    encoded = os.environ["BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY"]
    return base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4))


def seal_config(text: str, key: bytes) -> str:
    nonce = os.urandom(12)
    sealed = AESGCM(key).encrypt(nonce, text.encode("utf-8"), CONFIG_AAD)
    return "v1:" + base64.urlsafe_b64encode(nonce + sealed).rstrip(b"=").decode()


def open_config(value: str, key: bytes) -> str:
    if not value.startswith("v1:"):
        raise ValueError("Unsupported configuration encryption format")
    payload = base64.urlsafe_b64decode(value[3:] + "=" * (-len(value[3:]) % 4))
    return AESGCM(key).decrypt(payload[:12], payload[12:], CONFIG_AAD).decode("utf-8")


def _mapping(value) -> dict:
    if isinstance(value, str):
        value = json.loads(value)
    return value if isinstance(value, dict) else {}


def _list(value) -> list:
    if isinstance(value, str):
        value = json.loads(value)
    return value if isinstance(value, list) else []


def resolve_transport(dep, prompt_patterns) -> tuple[str, int]:
    """Netmiko device type and port for a switch.

    The switch's protocol decides SSH or Telnet. A device type set explicitly in the
    profile wins over the vendor default, and the profile's port only applies when the
    profile was written for the same protocol.
    """
    options = _mapping(_mapping(prompt_patterns).get("connection_options", {}))
    explicit = options.get("device_type")
    profile_type = str(explicit or LEGACY_DEVICE_TYPE)
    base = profile_type.removesuffix("_telnet")
    if not explicit:
        base = VENDOR_DEVICE_TYPES.get(dep["vendor"], base)
    protocol = dep["protocol"]
    device_type = base + "_telnet" if protocol == "telnet" else base
    profile_protocol = "telnet" if profile_type.endswith("_telnet") else "ssh"
    port = dep["port"]
    if not port and options.get("port") and profile_protocol == protocol:
        port = options["port"]
    return device_type, int(port or DEFAULT_PORTS[protocol])


def normalize_config(raw: str) -> str:
    lines = [line.rstrip() for line in raw.replace("\r\n", "\n").replace("\r", "\n").split("\n")]
    while lines and not lines[0]:
        lines.pop(0)
    while lines and not lines[-1]:
        lines.pop()
    return "\n".join(lines)


def config_fingerprint(text: str, ignore_patterns) -> str:
    """Hash of the significant lines, so timestamps alone never create a new version."""
    patterns = [re.compile(p) for p in (*VOLATILE_LINES, *ignore_patterns)]
    significant = [line for line in text.split("\n") if not any(p.search(line) for p in patterns)]
    return hashlib.sha256("\n".join(significant).encode("utf-8")).hexdigest()


class ConfigurationSession(NetmikoSwitchSession):
    def read_configuration(self, command: str, disable_paging: str | None) -> str:
        connection = self._require_connection()
        try:
            if disable_paging:
                connection.disable_paging(command=disable_paging)
            return connection.send_command(command, read_timeout=180)
        except (NetmikoTimeoutException, ReadTimeout) as exc:
            raise SwitchConnectionTimeoutError("Configuration read timed out") from exc
        except OSError as exc:
            raise SwitchSessionError("Configuration read failed") from exc


class BackupRejected(Exception):
    pass


def read_switch_configuration(dep, settings_builder) -> str:
    """Blocking Netmiko session; runs in a worker thread."""
    profile = SimpleNamespace(
        **{
            key: _mapping(dep[key])
            for key in (
                "login_prompt_patterns",
                "command_templates",
                "prompt_patterns",
                "pagination_rules",
                "parser_rules",
            )
        },
        error_patterns=_list(dep["error_patterns"]),
        success_patterns=[],
    )
    command = profile.command_templates.get("config_backup")
    if not isinstance(command, str) or not command.strip():
        raise BackupRejected("Command profile has no config_backup command")
    disable_paging = profile.command_templates.get("disable_paging") or next(
        (
            profile.pagination_rules.get(key)
            for key in ("disable_paging_command", "disable_paging")
            if profile.pagination_rules.get(key)
        ),
        None,
    )
    session = ConfigurationSession(settings_builder(dep, profile))
    try:
        session.connect()
        raw = session.read_configuration(command, disable_paging)
    finally:
        with suppress(Exception):
            session.close()
    text = normalize_config(raw or "")
    if len(text.encode("utf-8")) > MAX_CONFIG_BYTES:
        raise BackupRejected("Configuration is larger than 20 MB")
    if sum(1 for line in text.split("\n") if line.strip()) < 3:
        raise BackupRejected("Backup output is too short to be a configuration")
    if output_matches_any(text[:2000], profile.error_patterns):
        raise BackupRejected("Backup output matches an error pattern of the command profile")
    return text


def describe_failure(exc: BaseException) -> str:
    """A message safe to store and show: never connection secrets or raw device output."""
    if isinstance(exc, BackupRejected):
        return str(exc)
    if isinstance(exc, SwitchSessionError):
        return str(exc)
    if isinstance(exc, TimeoutError):
        return "Timed out"
    if isinstance(exc, ConnectionRefusedError):
        return "Connection refused"
    if isinstance(exc, OSError):
        return f"Network error ({exc.__class__.__name__})"
    return f"Unexpected error ({exc.__class__.__name__})"


async def upsert_alert(conn, kind, severity, switch_id, message, key):
    await conn.execute(
        """INSERT INTO bp_alerts(kind,severity,switch_id,message,dedupe_key) VALUES ($1,$2,$3,$4,$5)
        ON CONFLICT (dedupe_key) WHERE status<>'resolved'
        DO UPDATE SET last_seen_at=now(),message=EXCLUDED.message""",
        kind,
        severity,
        switch_id,
        message,
        key,
    )


async def resolve_alert(conn, key):
    await conn.execute(
        """UPDATE bp_alerts SET status='resolved',resolved_at=now()
        WHERE dedupe_key=$1 AND status<>'resolved'""",
        key,
    )


async def sweep_device_items(pool):
    async with pool.acquire() as conn:
        await conn.execute(
            """UPDATE bp_device_task_items SET status='failed',
            message='Worker stopped before finishing',
            finished_at=now(),lease_token=NULL,lease_until=NULL
            WHERE status='running' AND lease_until<now()"""
        )


async def claim_device_items(pool, limit):
    async with pool.acquire() as conn:
        return await conn.fetch(
            """UPDATE bp_device_task_items SET status='running',started_at=now(),
            lease_token=gen_random_uuid(),lease_until=now()+CASE kind
                WHEN 'config_backup' THEN interval '10 minutes' ELSE interval '2 minutes' END
            WHERE id IN (SELECT id FROM bp_device_task_items WHERE status='queued' ORDER BY id
                         FOR UPDATE SKIP LOCKED LIMIT $1)
            RETURNING id,lease_token,kind,switch_id""",
            limit,
        )


async def finish_item(conn, item, status, message=None, *, changed=None, backup_id=None) -> bool:
    result = await conn.execute(
        """UPDATE bp_device_task_items SET status=$3,message=$4,changed=$5,backup_id=$6,
        finished_at=now(),lease_token=NULL,lease_until=NULL
        WHERE id=$1 AND lease_token=$2 AND status='running'""",
        item["id"],
        item["lease_token"],
        status,
        message,
        changed,
        backup_id,
    )
    return result == "UPDATE 1"


async def load_device(pool, switch_id):
    async with pool.acquire() as conn:
        return await conn.fetchrow(
            """SELECT s.id,host(s.management_ip) AS management_ip,s.vendor,s.protocol,s.port,
            s.is_enabled,
            c.username,c.encrypted_password,c.encrypted_secret,c.is_active AS credential_active,
            p.is_active AS profile_active,p.login_prompt_patterns,p.command_templates,
            p.prompt_patterns,
            p.pagination_rules,p.error_patterns,p.parser_rules
            FROM bp_switches s JOIN bp_credentials c ON c.id=s.credential_id
            JOIN bp_command_profiles p ON p.id=s.command_profile_id WHERE s.id=$1""",
            switch_id,
        )


async def check_reachability(pool, item, dep):
    _, port = resolve_transport(dep, dep["prompt_patterns"])
    started = time.monotonic()
    latency, error = None, None
    try:
        _, writer = await asyncio.wait_for(
            asyncio.open_connection(dep["management_ip"], port), timeout=REACHABILITY_TIMEOUT
        )
        latency = round((time.monotonic() - started) * 1000)
        writer.close()
        with suppress(Exception):
            await writer.wait_closed()
    except (OSError, TimeoutError) as exc:
        error = describe_failure(exc)
    reachable = error is None
    key = f"device_unreachable:{dep['id']}"
    async with pool.acquire() as conn, conn.transaction():
        if not await finish_item(conn, item, "succeeded", None if reachable else error):
            return
        await conn.execute(
            """UPDATE bp_switches SET reachable=$2,reachability_checked_at=now(),
            reachability_changed_at=CASE WHEN reachable IS DISTINCT FROM $2 THEN now()
                ELSE reachability_changed_at END,
            reachability_latency_ms=$3,reachability_error=$4 WHERE id=$1""",
            dep["id"],
            reachable,
            latency,
            error,
        )
        if reachable:
            await resolve_alert(conn, key)
        else:
            await upsert_alert(
                conn,
                "device_unreachable",
                "warning",
                dep["id"],
                f"Port {port} unreachable: {error}",
                key,
            )


async def back_up(pool, item, dep, settings_builder, key):
    alert_key = f"backup_failed:{dep['id']}"
    try:
        if not (dep["credential_active"] and dep["profile_active"]):
            raise BackupRejected("Credential or command profile is disabled")
        text = await asyncio.to_thread(read_switch_configuration, dep, settings_builder)
    except Exception as exc:  # every failure is recorded on the item, never raised past it
        message = describe_failure(exc)
        if not isinstance(exc, (BackupRejected, SwitchSessionError, OSError)):
            logger.exception("backup of switch %s failed", dep["id"])
        async with pool.acquire() as conn, conn.transaction():
            if await finish_item(conn, item, "failed", message):
                await conn.execute(
                    "UPDATE bp_switches SET config_checked_at=now(),config_error=$2 WHERE id=$1",
                    dep["id"],
                    message,
                )
                await upsert_alert(
                    conn,
                    "backup_failed",
                    "warning",
                    dep["id"],
                    f"Backup failed: {message}",
                    alert_key,
                )
        return
    ignore = _mapping(dep["parser_rules"]).get("config_ignore_patterns", [])
    fingerprint = config_fingerprint(text, ignore if isinstance(ignore, list) else [])
    sealed = seal_config(text, key)
    async with pool.acquire() as conn, conn.transaction():
        latest = await conn.fetchval(
            """SELECT content_sha256 FROM bp_config_backups WHERE switch_id=$1
            ORDER BY captured_at DESC,id DESC LIMIT 1""",
            dep["id"],
        )
        changed, backup_id = latest != fingerprint, None
        if changed:
            backup_id = await conn.fetchval(
                """INSERT INTO bp_config_backups(switch_id,content_sha256,size_bytes,line_count,
                encrypted_content)
                VALUES ($1,$2,$3,$4,$5) RETURNING id""",
                dep["id"],
                fingerprint,
                len(text.encode("utf-8")),
                text.count("\n") + 1,
                sealed,
            )
        message = "New configuration version" if changed else "Configuration unchanged"
        if not await finish_item(
            conn, item, "succeeded", message, changed=changed, backup_id=backup_id
        ):
            raise RuntimeError("lost device item lease")  # rolls the new version back
        await conn.execute(
            "UPDATE bp_switches SET config_checked_at=now(),config_error=NULL WHERE id=$1",
            dep["id"],
        )
        await resolve_alert(conn, alert_key)


async def process_device_item(pool, item, settings_builder, key):
    try:
        dep = await load_device(pool, item["switch_id"])
        if dep is None or not dep["is_enabled"]:
            async with pool.acquire() as conn:
                await finish_item(conn, item, "skipped", "Switch is disabled")
            return
        if item["kind"] == "reachability":
            await check_reachability(pool, item, dep)
        else:
            await back_up(pool, item, dep, settings_builder, key)
    except Exception:
        logger.exception("device item %s failed", item["id"])
        with suppress(Exception):
            async with pool.acquire() as conn:
                await finish_item(conn, item, "failed", "Unexpected worker error")


async def device_loop(pool, concurrency, settings_builder):
    """Runs up to `concurrency` device items at once, independently of release jobs."""
    key = credential_key()
    running: set[asyncio.Task] = set()
    last_sweep = 0.0
    while True:
        try:
            if time.monotonic() - last_sweep > 30:
                await sweep_device_items(pool)
                last_sweep = time.monotonic()
            free = concurrency - len(running)
            for item in await claim_device_items(pool, free) if free > 0 else []:
                task = asyncio.create_task(process_device_item(pool, item, settings_builder, key))
                running.add(task)
                task.add_done_callback(running.discard)
        except Exception:
            logger.exception("device queue unavailable")
            await asyncio.sleep(5)
            continue
        if running:
            await asyncio.wait(running, timeout=1, return_when=asyncio.FIRST_COMPLETED)
        else:
            await asyncio.sleep(1)
