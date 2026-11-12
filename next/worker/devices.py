"""Device operations: connection tests, configuration backups and the release switches' network
reads. Whether switches are up is Zabbix's to watch, not Bind Plane's.

All of them only read from switches, so a lost lease fails the item instead of risking a
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
    SwitchAuthenticationError,
    SwitchConnectionTimeoutError,
    SwitchSessionError,
)
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from netmiko.exceptions import NetmikoTimeoutException, ReadTimeout
from worker.networks import IncompleteConfiguration, configuration_networks

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
PORT_TIMEOUT = 3.0
# Where a configuration names the software it runs, near its top: Huawei VRP
# ("!Software Version V200R019C10SPC500"), H3C Comware (" version 7.1.070, Release 6616")
# and Ruijie or Cisco ("version 11.4(1)B74P2"). The first pattern that finds a line wins.
OS_VERSION_PATTERNS = (r"^\s*!\s*Software Version\s+(\S+)", r"^\s*version\s+(\S.*?)\s*$")
OS_VERSION_LINES = 40


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


def os_version(text: str, parser_rules=None) -> str | None:
    """The software version a configuration names near its top, or None when it names none.

    A command profile can give its own `os_version_patterns`, each with one group; a pattern that
    does not compile is skipped.
    """
    own = _mapping(parser_rules).get("os_version_patterns")
    compiled = []
    for pattern in own if isinstance(own, list) and own else OS_VERSION_PATTERNS:
        with suppress(re.error, TypeError):
            compiled.append(re.compile(pattern))
    head = text.split("\n", OS_VERSION_LINES)[:OS_VERSION_LINES]
    for pattern in compiled:
        for line in head:
            found = pattern.search(line)
            if found and found.groups() and found.group(1):
                return found.group(1).strip()[:100]
    return None


def config_fingerprint(text: str, ignore_patterns) -> str:
    """Hash of the significant lines, so timestamps alone never create a new version."""
    patterns = [re.compile(p) for p in (*VOLATILE_LINES, *ignore_patterns)]
    significant = [line for line in text.split("\n") if not any(p.search(line) for p in patterns)]
    return hashlib.sha256("\n".join(significant).encode("utf-8")).hexdigest()


# A full page on a switch that cannot turn paging off: "  ---- More ----" on H3C and Huawei,
# " --More-- " on Ruijie. After a space the switch wipes it with cursor moves, backspaces and
# spaces.
MORE_PROMPT = r"-+ ?[Mm]ore ?-+"
# Spaces count as wiping only between cursor moves or backspaces, so the indentation of the line
# that follows is kept.
MORE_AND_WIPE = re.compile(r"[ \t]*" + MORE_PROMPT + r"(?:[ \t]*(?:\x1b\[\d*[A-Za-z]|\x08)+)*")
ANSI_ESCAPE = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")
CONFIG_READ_SECONDS = 180


class ConfigurationSession(NetmikoSwitchSession):
    def read_configuration(
        self, command: str, disable_paging: str | None, elevate_privilege: str | None = None
    ) -> str:
        """Read the running configuration. Many switches show it only at the privileged level
        (H3C and Huawei after super, Ruijie after enable), so enter that first when the profile
        has the command, the same way a release does. Paging is turned off when the profile
        can; old switches cannot, so a More prompt is answered with a space, as jbxt did."""
        connection = self._require_connection()
        try:
            if elevate_privilege:
                self.elevate_privilege(elevate_privilege)
            if disable_paging:
                connection.disable_paging(command=disable_paging)
            return read_paged(connection, command)
        except (NetmikoTimeoutException, ReadTimeout) as exc:
            raise SwitchConnectionTimeoutError("Configuration read timed out") from exc
        except OSError as exc:
            raise SwitchSessionError("Configuration read failed") from exc


def read_paged(connection, command: str, read_timeout: float = CONFIG_READ_SECONDS) -> str:
    """Send a command and read its whole output up to the prompt, pressing space at each More
    prompt. Returns the output without the echoed command, the prompt, the More prompts or the
    terminal codes that wipe them."""
    prompt = re.compile(re.escape(connection.base_prompt) + r"[^\n]*[>#\]]\s*$")
    page_end = re.compile(MORE_PROMPT + r"\s*$")
    connection.write_channel(connection.normalize_cmd(command))
    deadline = time.monotonic() + read_timeout
    output = ""
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise ReadTimeout("Configuration read timed out")
        output += connection.read_until_pattern(
            pattern=f"(?:{prompt.pattern})|(?:{page_end.pattern})", read_timeout=remaining
        )
        if prompt.search(output):
            break
        connection.write_channel(" ")
    lines = ANSI_ESCAPE.sub("", MORE_AND_WIPE.sub("", output)).replace("\r\n", "\n").split("\n")
    if lines and command.strip() in lines[0]:
        lines = lines[1:]
    if lines and prompt.search(lines[-1]):
        lines = lines[:-1]
    return "\n".join(lines)


class BackupRejected(Exception):
    pass


def switch_profile(dep) -> SimpleNamespace:
    """The parts of a switch's command profile that a read-only session needs."""
    return SimpleNamespace(
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


def sign_in(dep, settings_builder) -> None:
    """Blocking Netmiko login and logout, sending no command; runs in a worker thread."""
    session = NetmikoSwitchSession(settings_builder(dep, switch_profile(dep)))
    try:
        session.connect()
    finally:
        with suppress(Exception):
            session.close()


def read_switch_configuration(dep, settings_builder) -> str:
    """Blocking Netmiko session; runs in a worker thread."""
    profile = switch_profile(dep)
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
    elevate = profile.command_templates.get("super")
    session = ConfigurationSession(settings_builder(dep, profile))
    try:
        session.connect()
        raw = session.read_configuration(
            command,
            disable_paging,
            elevate if isinstance(elevate, str) and elevate.strip() else None,
        )
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


class NetworksKept(Exception):
    """A read whose result would wipe out a switch's networks, so they are kept as they were."""


def describe_failure(exc: BaseException) -> str:
    """A message safe to store and show: never connection secrets or raw device output."""
    if isinstance(exc, (BackupRejected, IncompleteConfiguration, NetworksKept)):
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
                WHEN 'config_backup' THEN interval '10 minutes'
                WHEN 'network_read' THEN interval '10 minutes'
                WHEN 'login_check' THEN interval '3 minutes' ELSE interval '2 minutes' END
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
            """SELECT s.id,s.name,host(s.management_ip) AS management_ip,s.vendor,s.protocol,s.port,
            s.is_enabled,s.handles_release,
            c.username,c.encrypted_password,c.encrypted_secret,c.is_active AS credential_active,
            p.is_active AS profile_active,p.login_prompt_patterns,p.command_templates,
            p.prompt_patterns,
            p.pagination_rules,p.error_patterns,p.parser_rules
            FROM bp_switches s JOIN bp_credentials c ON c.id=s.credential_id
            JOIN bp_command_profiles p ON p.id=s.command_profile_id WHERE s.id=$1""",
            switch_id,
        )


async def probe_port(dep) -> tuple[int, str | None]:
    """Opens and closes one TCP connection to the management port: (port, error)."""
    _, port = resolve_transport(dep, dep["prompt_patterns"])
    try:
        _, writer = await asyncio.wait_for(
            asyncio.open_connection(dep["management_ip"], port), timeout=PORT_TIMEOUT
        )
    except (OSError, TimeoutError) as exc:
        return port, describe_failure(exc)
    writer.close()
    with suppress(Exception):
        await writer.wait_closed()
    return port, None


async def record_login(conn, switch_id, error):
    await conn.execute(
        """UPDATE bp_switches SET login_ok=$2,login_checked_at=now(),login_error=$3
        WHERE id=$1""",
        switch_id,
        error is None,
        error,
    )


async def check_login(pool, item, dep, settings_builder):
    """Connection test: the port must answer, then the switch's credential must sign in.

    The port is tried first only to say why a test failed; either failure is a failed connection.
    """
    port, error = await probe_port(dep)
    login_error = None
    if error is None:
        try:
            if not (dep["credential_active"] and dep["profile_active"]):
                raise BackupRejected("Credential or command profile is disabled")
            await asyncio.to_thread(sign_in, dep, settings_builder)
        except Exception as exc:  # every failure is recorded on the item, never raised past it
            login_error = describe_failure(exc)
            if not isinstance(exc, (BackupRejected, SwitchSessionError, OSError)):
                logger.exception("connection test of switch %s failed", dep["id"])
    if error is not None:
        message = f"Port {port} unreachable: {error}"
    else:
        message = login_error or "Signed in"
    status = "succeeded" if message == "Signed in" else "failed"
    async with pool.acquire() as conn, conn.transaction():
        if await finish_item(conn, item, status, message):
            await record_login(conn, dep["id"], None if status == "succeeded" else message)


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
                if isinstance(exc, SwitchAuthenticationError):
                    await record_login(conn, dep["id"], message)
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
            """UPDATE bp_switches SET config_checked_at=now(),config_succeeded_at=now(),
            config_error=NULL,os_version=$2 WHERE id=$1""",
            dep["id"],
            os_version(text, dep["parser_rules"]),
        )
        await record_login(conn, dep["id"], None)  # the backup signed in
        await resolve_alert(conn, alert_key)
    if dep["handles_release"]:
        # The configuration in hand is as current as a network read's, so it saves one.
        async with pool.acquire() as conn, conn.transaction():
            try:
                await sync_networks(conn, dep, text, requested=False)
            except (IncompleteConfiguration, NetworksKept) as exc:
                await record_network_failure(conn, dep, describe_failure(exc))


def network_plan(existing, found, *, requested: bool):
    """What a read changes: (added, removed, kept) by CIDR.

    Networks typed in before they were read from configuration (no interface) give way to the
    first read. A read that finds nothing, or that would remove most of the networks read before,
    keeps them all: a cut-short session or a configuration in an unknown format must not empty
    the routing. Someone who asked for this read has seen the switch and may accept the latter.
    """
    found_by_cidr = {network.cidr: network for network in found}
    before = {row["cidr"]: row for row in existing}
    read_before = [cidr for cidr, row in before.items() if row["interface"] is not None]
    removed = sorted(cidr for cidr in before if cidr not in found_by_cidr)
    if not found and before:
        raise NetworksKept("No interface addresses in the configuration; networks kept")
    lost = [cidr for cidr in read_before if cidr not in found_by_cidr]
    if not requested and len(lost) >= 3 and len(lost) * 2 > len(read_before):
        raise NetworksKept(
            f"The configuration would remove {len(lost)} of {len(read_before)} networks; "
            "networks kept"
        )
    added = [found_by_cidr[cidr] for cidr in sorted(found_by_cidr) if cidr not in before]
    kept = [found_by_cidr[cidr] for cidr in sorted(found_by_cidr) if cidr in before]
    return added, removed, kept


async def sync_networks(conn, dep, text, *, requested: bool) -> str:
    """Makes the switch's networks those of its configuration and records the change."""
    found = configuration_networks(text)
    existing = await conn.fetch(
        "SELECT id,cidr::text AS cidr,interface FROM bp_networks WHERE switch_id=$1 FOR UPDATE",
        dep["id"],
    )
    added, removed, kept = network_plan(existing, found, requested=requested)
    if removed:
        await conn.execute(
            "DELETE FROM bp_networks WHERE switch_id=$1 AND cidr::text = ANY($2::text[])",
            dep["id"],
            removed,
        )
    for network in kept:
        await conn.execute(
            """UPDATE bp_networks SET interface=$3,vlan=$4,description=$5,is_enabled=true,
            is_validated=true WHERE switch_id=$1 AND cidr=$2::cidr""",
            dep["id"],
            network.cidr,
            network.interface,
            network.vlan,
            network.description,
        )
    for network in added:
        await conn.execute(
            """INSERT INTO bp_networks(switch_id,cidr,interface,vlan,description,is_validated)
            VALUES ($1,$2::cidr,$3,$4,$5,true)""",
            dep["id"],
            network.cidr,
            network.interface,
            network.vlan,
            network.description,
        )
    if added or removed:
        # The worker acts for nobody in particular, so the record names no person.
        await conn.execute(
            """INSERT INTO bp_asset_events(actor_email,action,object_id,object_name,changes)
            VALUES ('','switch.networks',$1,$2,$3::jsonb)""",
            dep["id"],
            dep["name"],
            json.dumps({"added": [n.cidr for n in added], "removed": removed}),
        )
    await conn.execute(
        """UPDATE bp_switches SET networks_checked_at=now(),networks_succeeded_at=now(),
        networks_error=NULL WHERE id=$1""",
        dep["id"],
    )
    await resolve_alert(conn, f"network_read_failed:{dep['id']}")
    if not (added or removed):
        return "Networks unchanged"
    return f"Networks changed: {len(added)} added, {len(removed)} removed"


async def record_network_failure(conn, dep, message):
    await conn.execute(
        "UPDATE bp_switches SET networks_checked_at=now(),networks_error=$2 WHERE id=$1",
        dep["id"],
        message,
    )
    await upsert_alert(
        conn,
        "network_read_failed",
        "warning",
        dep["id"],
        f"Network read failed: {message}",
        f"network_read_failed:{dep['id']}",
    )


async def read_networks(pool, item, dep, settings_builder):
    """Reads a release switch's configuration and makes its networks match it."""
    async with pool.acquire() as conn:
        requested = await conn.fetchval(
            """SELECT t.actor_id IS NOT NULL FROM bp_device_tasks t
            JOIN bp_device_task_items i ON i.task_id=t.id WHERE i.id=$1""",
            item["id"],
        )
    try:
        if not dep["handles_release"]:
            raise BackupRejected("Switch does not handle releases")
        if not (dep["credential_active"] and dep["profile_active"]):
            raise BackupRejected("Credential or command profile is disabled")
        text = await asyncio.to_thread(read_switch_configuration, dep, settings_builder)
    except Exception as exc:  # every failure is recorded on the item, never raised past it
        message = describe_failure(exc)
        if not isinstance(exc, (BackupRejected, SwitchSessionError, OSError)):
            logger.exception("network read of switch %s failed", dep["id"])
        async with pool.acquire() as conn, conn.transaction():
            if await finish_item(conn, item, "failed", message) and dep["handles_release"]:
                await record_network_failure(conn, dep, message)
                if isinstance(exc, SwitchAuthenticationError):
                    await record_login(conn, dep["id"], message)
        return
    async with pool.acquire() as conn, conn.transaction():
        try:
            async with conn.transaction():  # a kept read rolls back only its own changes
                message = await sync_networks(conn, dep, text, requested=bool(requested))
            status = "succeeded"
        except (IncompleteConfiguration, NetworksKept) as exc:
            message, status = describe_failure(exc), "failed"
            await record_network_failure(conn, dep, message)
        if not await finish_item(conn, item, status, message):
            raise RuntimeError("lost device item lease")  # rolls the networks back
        await record_login(conn, dep["id"], None)  # the read signed in
        await conn.execute(
            "UPDATE bp_switches SET os_version=$2 WHERE id=$1",
            dep["id"],
            os_version(text, dep["parser_rules"]),
        )


async def process_device_item(pool, item, settings_builder, key):
    try:
        dep = await load_device(pool, item["switch_id"])
        if dep is None or not dep["is_enabled"]:
            async with pool.acquire() as conn:
                await finish_item(conn, item, "skipped", "Switch is disabled")
            return
        if item["kind"] == "login_check":
            await check_login(pool, item, dep, settings_builder)
        elif item["kind"] == "network_read":
            await read_networks(pool, item, dep, settings_builder)
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
