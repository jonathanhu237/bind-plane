"""Database-backed, fenced switch executor. Never run inside the HTTP process."""

import asyncio
import base64
import json
import logging
import os
from contextlib import suppress
from ipaddress import IPv4Address
from types import SimpleNamespace

import asyncpg
from bind_plane.db.models import ArpEntryType
from bind_plane.security.credentials import encrypt_secret
from bind_plane.services.command_profiles import (
    classify_release_result,
    observation_to_dict,
    parse_arp_observation,
    render_commands,
)
from bind_plane.worker.release_executor import build_connection_settings_for_switch
from bind_plane.worker.sessions import NetmikoSwitchSession, SwitchConnectionTimeoutError
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

logger = logging.getLogger("bind_plane.worker")


async def sweep_expired(pool):
    # A command may have been sent. Never requeue a claimed job automatically.
    async with pool.acquire() as conn, conn.transaction():
        rows = await conn.fetch("""UPDATE bp_release_jobs SET status='needs_manual_confirmation',
                phase='finished', finished_at=now(), updated_at=now(),
                error_message='Worker lease expired; switch state is uncertain',
                lease_token=NULL,lease_until=NULL
                WHERE status='running' AND lease_until<now() RETURNING id""")
        for row in rows:
            await conn.execute(
                """INSERT INTO bp_release_events(job_id,phase,status,message)
                    VALUES($1,'finished','needs_manual_confirmation','Worker lease expired')""",
                row["id"],
            )


async def claim(pool):
    async with pool.acquire() as conn, conn.transaction():
        row = await conn.fetchrow("""SELECT id FROM bp_release_jobs WHERE status='queued'
                ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1""")
        if row is None:
            return None
        return await conn.fetchrow(
            """UPDATE bp_release_jobs SET status='running',phase='connecting',
                started_at=now(),updated_at=now(),lease_token=gen_random_uuid(),
                lease_until=now()+interval '90 seconds' WHERE id=$1
                RETURNING id,lease_token,kind,target_ip::text,force,switch_id,command_profile_id""",
            row["id"],
        )


async def checkpoint(pool, job, *, phase, status="running", message=None, **changes):
    allowed = {
        "before_state",
        "after_state",
        "result",
        "raw_before_output",
        "raw_release_output",
        "raw_after_output",
        "error_message",
    }
    if set(changes) - allowed:
        raise ValueError("unsupported checkpoint field")
    # A stale executor cannot overwrite a job whose lease has been revoked.
    columns = ", ".join(f"{key}=${i}" for i, key in enumerate(changes, start=5))
    values = [
        json.dumps(value) if key in {"before_state", "after_state", "result"} else value
        for key, value in changes.items()
    ]
    async with pool.acquire() as conn, conn.transaction():
        sql = f"""UPDATE bp_release_jobs SET phase=$3,status=$4,updated_at=now(),
                lease_until=CASE WHEN $4='running' THEN now()+interval '90 seconds' ELSE NULL END,
                lease_token=CASE WHEN $4='running' THEN lease_token ELSE NULL END,
                finished_at=CASE WHEN $4 IN ('running','waiting_confirmation')
                    THEN NULL ELSE now() END
                {"," + columns if columns else ""} WHERE id=$1
                AND lease_token=$2 AND status='running'
                AND lease_until>now() RETURNING id"""
        row = await conn.fetchrow(sql, job["id"], job["lease_token"], phase, status, *values)
        if row is None:
            raise RuntimeError("lost execution lease; stop switch interaction")
        await conn.execute(
            """INSERT INTO bp_release_events(job_id,phase,status,message)
                VALUES($1,$2,$3,$4)""",
            job["id"],
            phase,
            status,
            message,
        )


async def heartbeat(pool, job):
    while True:
        await asyncio.sleep(15)
        async with pool.acquire() as conn:
            updated = await conn.execute(
                """UPDATE bp_release_jobs SET lease_until=now()+interval '90 seconds'
                WHERE id=$1 AND lease_token=$2 AND status='running' AND lease_until>now()""",
                job["id"],
                job["lease_token"],
            )
            if updated != "UPDATE 1":
                raise RuntimeError("execution lease lost")


def legacy_encrypted_secret(value):
    if not value:
        return None
    if not value.startswith("v1:"):
        raise ValueError("Unsupported credential encryption format")
    encoded_key = os.environ["BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY"]
    key = base64.urlsafe_b64decode(encoded_key + "=" * (-len(encoded_key) % 4))
    payload = base64.urlsafe_b64decode(value[3:] + "=" * (-len(value[3:]) % 4))
    plaintext = AESGCM(key).decrypt(payload[:12], payload[12:], None).decode("utf-8")
    return encrypt_secret(plaintext)


async def load_dependencies(pool, job):
    async with pool.acquire() as conn:
        return await conn.fetchrow(
            """SELECT s.management_ip::text,s.is_enabled,c.username,c.encrypted_password,
            c.encrypted_secret,c.is_active AS credential_active,p.is_active AS profile_active,
            p.login_prompt_patterns,p.command_templates,p.prompt_patterns,p.pagination_rules,
            p.success_patterns,p.error_patterns,p.parser_rules
            FROM bp_switches s JOIN bp_credentials c ON c.id=s.credential_id
            JOIN bp_command_profiles p ON p.id=s.command_profile_id
            WHERE s.id=$1 AND p.id=$2""",
            job["switch_id"],
            job["command_profile_id"],
        )


def device_task(job, dep, checkpoint_sync):
    """Run all Netmiko operations in one dedicated thread; checkpoints are durable."""
    target = IPv4Address(job["target_ip"])
    if not dep or not (dep["is_enabled"] and dep["credential_active"] and dep["profile_active"]):
        raise ValueError("switch, credential or profile unavailable")
    profile = SimpleNamespace(
        **{
            k: json.loads(dep[k]) if isinstance(dep[k], str) else dep[k]
            for k in (
                "login_prompt_patterns",
                "command_templates",
                "prompt_patterns",
                "pagination_rules",
                "success_patterns",
                "error_patterns",
                "parser_rules",
            )
        }
    )
    switch = SimpleNamespace(management_ip=dep["management_ip"])
    credential = SimpleNamespace(
        username=dep["username"],
        encrypted_password=legacy_encrypted_secret(dep["encrypted_password"]),
        encrypted_secret=legacy_encrypted_secret(dep["encrypted_secret"]),
    )
    settings = build_connection_settings_for_switch(
        switch=switch, credential=credential, profile=profile
    )
    commands = render_commands(
        profile.command_templates, target, pagination_rules=profile.pagination_rules
    )
    session = NetmikoSwitchSession(settings)
    command_may_have_run = False
    try:
        session.connect()
        checkpoint_sync(phase="querying_before")
        raw_before = session.query_before(commands)
        before = parse_arp_observation(
            target_ip=target, output=raw_before, parser_rules=profile.parser_rules
        )
        checkpoint_sync(
            phase="querying_before",
            raw_before_output=raw_before,
            before_state=observation_to_dict(before),
        )
        if before.entry_type == ArpEntryType.UNKNOWN:
            checkpoint_sync(
                phase="finished",
                status="needs_manual_confirmation",
                message="Pre-query could not be parsed",
            )
            return
        if job["kind"] == "pre_release_query":
            if before.entry_type == ArpEntryType.MISSING and not job["force"]:
                checkpoint_sync(
                    phase="finished",
                    status="cancelled",
                    message="No record; operator must stop",
                    result={"preparation_status": "stopped_no_record"},
                )
            else:
                checkpoint_sync(
                    phase="waiting_confirmation",
                    status="waiting_confirmation",
                    message="Pre-query ready for confirmation",
                    result={"preparation_status": "ready"},
                )
            return
        if before.entry_type == ArpEntryType.MISSING and not job["force"]:
            checkpoint_sync(
                phase="finished", status="cancelled", message="No record; release stopped"
            )
            return
        checkpoint_sync(phase="releasing")
        command_may_have_run = True
        raw_release = session.release(commands)
        checkpoint_sync(phase="querying_after", raw_release_output=raw_release)
        raw_after = session.query_after(commands)
        checkpoint_sync(phase="classifying", raw_after_output=raw_after)
        after = parse_arp_observation(
            target_ip=target, output=raw_after, parser_rules=profile.parser_rules
        )
        classified = classify_release_result(
            release_output=raw_release,
            after_observation=after,
            error_patterns=profile.error_patterns,
            success_patterns=profile.success_patterns,
        )
        checkpoint_sync(
            phase="finished",
            status=classified.status.value,
            message=classified.message,
            after_state=observation_to_dict(after),
            raw_after_output=raw_after,
            result={"message": classified.message, **classified.details},
        )
    except Exception as exc:
        # After entry into the release phase even a timeout cannot prove command non-execution.
        status = (
            "needs_manual_confirmation"
            if command_may_have_run
            else ("timeout" if isinstance(exc, SwitchConnectionTimeoutError) else "failed")
        )
        partial = getattr(exc, "partial_output", None)
        details = (
            {"raw_release_output": partial}
            if command_may_have_run and isinstance(partial, str)
            else {}
        )
        checkpoint_sync(
            phase="finished",
            status=status,
            message=exc.__class__.__name__,
            error_message=f"Switch execution: {exc.__class__.__name__}",
            **details,
        )
    finally:
        try:
            session.close()
        except Exception:
            logger.exception("switch session close failed")


async def execute(pool, job):
    loop = asyncio.get_running_loop()
    keepalive = asyncio.create_task(heartbeat(pool, job))

    def save(**kwargs):
        future = asyncio.run_coroutine_threadsafe(checkpoint(pool, job, **kwargs), loop)
        future.result(timeout=20)

    try:
        dep = await load_dependencies(pool, job)
        await asyncio.to_thread(device_task, job, dep, save)
    except Exception:
        logger.exception("executor failed for job %s", job["id"])
        try:
            await checkpoint(
                pool,
                job,
                phase="finished",
                status="needs_manual_confirmation",
                message="Executor failure; switch state uncertain",
            )
        except Exception:
            logger.exception("could not finalize job %s", job["id"])
    finally:
        keepalive.cancel()
        with suppress(asyncio.CancelledError):
            await keepalive


async def main():
    logging.basicConfig(level=logging.INFO)
    pool = await asyncpg.create_pool(os.environ["EXECUTOR_DATABASE_URL"], min_size=1, max_size=4)
    try:
        while True:
            await sweep_expired(pool)
            job = await claim(pool)
            if job is None:
                await asyncio.sleep(1)
            else:
                await execute(pool, job)
    finally:
        await pool.close()


if __name__ == "__main__":
    asyncio.run(main())
