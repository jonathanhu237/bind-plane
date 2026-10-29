"""Backups and a full release against a fake Telnet switch, through real Netmiko sessions."""

import asyncio
import base64
import json
import os
import random

import asyncpg
import pytest
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from worker.devices import (
    claim_device_items,
    credential_key,
    normalize_config,
    open_config,
    process_device_item,
)
from worker.fake_switch import DEFAULT_CONFIG, FakeSwitch
from worker.main import claim, connection_settings, execute

PROFILE_TEMPLATES = {
    "config_backup": "display current-configuration",
    "single_arp_query": "display arp all | include $ip",
    "arp_release": "undo arp static $ip",
    "config": "system-view",
    "exit_config": "return",
}
PARSER_RULES = {
    "arp_entry_regex": (
        r"(?P<ip>\d+\.\d+\.\d+\.\d+)\s+"
        r"(?P<mac>[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4})\s+(?P<type>static|dynamic)"
    ),
    "static_type_values": ["static"],
    "dynamic_type_values": ["dynamic"],
    "missing_patterns": ["Total:0"],
}


def api_sealed(secret: str) -> str:
    """A credential exactly as the API stores it."""
    nonce = os.urandom(12)
    payload = AESGCM(credential_key()).encrypt(nonce, secret.encode(), None)
    return "v1:" + base64.urlsafe_b64encode(nonce + payload).rstrip(b"=").decode()


@pytest.mark.asyncio
async def test_backups_and_a_release_against_a_fake_switch(tmp_path):
    dsn = os.getenv("EXECUTOR_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated test database required")
    key = credential_key()
    running = tmp_path / "running.cfg"
    running.write_text(DEFAULT_CONFIG)
    # Every 127/8 address is loopback, so each run gets its own unique management IP.
    ip = f"127.0.{random.randint(1, 254)}.{random.randint(2, 254)}"
    fake = FakeSwitch(
        "netops",
        "fake-pass",
        config_file=str(running),
        static_arp={"10.20.30.40": "00e0-fc12-3456"},
    )
    server = await asyncio.start_server(fake.handle, ip, 0)
    port = server.sockets[0].getsockname()[1]
    pool = await asyncpg.create_pool(dsn, min_size=1, max_size=6)
    try:
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_release_jobs SET status='cancelled' WHERE status='queued'"
            )
            await conn.execute(
                "UPDATE bp_device_task_items SET status='skipped' WHERE status='queued'"
            )
            actor = await conn.fetchval(
                """INSERT INTO auth_users(name,email,email_canonical,password_hash)
                VALUES ('E2E',$1,$1,'x') RETURNING id""",
                f"e2e-{ip}@test.invalid",
            )
            credential = await conn.fetchval(
                """INSERT INTO bp_credentials(name,username,encrypted_password)
                VALUES ($1,'netops',$2) RETURNING id""",
                f"fake-{ip}",
                api_sealed("fake-pass"),
            )
            profile = await conn.fetchval(
                """INSERT INTO bp_command_profiles(name,command_templates,error_patterns,
                parser_rules)
                VALUES ($1,$2,'["Error:"]',$3) RETURNING id""",
                f"fake-{ip}",
                json.dumps(PROFILE_TEMPLATES),
                json.dumps(PARSER_RULES),
            )
            switch = await conn.fetchval(
                """INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id,
                vendor,protocol,port)
                VALUES ('FAKE-SW',$1,$2,$3,'huawei','telnet',$4) RETURNING id""",
                ip,
                credential,
                profile,
                port,
            )

        async def run(kind):
            async with pool.acquire() as conn:
                task = await conn.fetchval(
                    "INSERT INTO bp_device_tasks(kind,scope_label) VALUES ($1,'e2e') RETURNING id",
                    kind,
                )
                await conn.execute(
                    "INSERT INTO bp_device_task_items(task_id,kind,switch_id) VALUES ($1,$2,$3)",
                    task,
                    kind,
                    switch,
                )
            [item] = await claim_device_items(pool, 1)
            await process_device_item(pool, item, connection_settings, key)
            async with pool.acquire() as conn:
                return await conn.fetchrow(
                    "SELECT status,message,changed,backup_id FROM bp_device_task_items WHERE id=$1",
                    item["id"],
                )

        reach = await run("reachability")
        assert reach["status"] == "succeeded" and reach["message"] is None
        first = await run("config_backup")
        assert (first["status"], first["changed"]) == ("succeeded", True), first["message"]
        async with pool.acquire() as conn:
            sealed = await conn.fetchval(
                "SELECT encrypted_content FROM bp_config_backups WHERE id=$1", first["backup_id"]
            )
            assert "irreversible-cipher" not in sealed
            assert open_config(sealed, key) == normalize_config(DEFAULT_CONFIG)
        second = await run("config_backup")
        assert (second["changed"], second["backup_id"]) == (False, None)
        running.write_text(DEFAULT_CONFIG.replace("vlan batch 10 20", "vlan batch 10 20 30"))
        third = await run("config_backup")
        assert third["changed"] and third["backup_id"] > first["backup_id"]

        async with pool.acquire() as conn:
            pinned = await conn.fetchrow(
                """SELECT s.version AS sv,p.version AS pv FROM bp_switches s
                JOIN bp_command_profiles p ON p.id=s.command_profile_id WHERE s.id=$1""",
                switch,
            )
            prequery = await conn.fetchval(
                """INSERT INTO bp_release_jobs(target_ip,kind,reason,status,phase,actor_id,
                actor_email,switch_id,command_profile_id,switch_version,command_profile_version,
                switch_name,switch_management_ip)
                VALUES ('10.20.30.40','pre_release_query','temporary_test','queued','queued',$1,
                'e2e@test.invalid',
                $2,$3,$4,$5,'FAKE-SW',$6::inet) RETURNING id""",
                actor,
                switch,
                profile,
                pinned["sv"],
                pinned["pv"],
                ip,
            )
        job = await claim(pool)
        assert job["id"] == prequery
        await execute(pool, job)
        async with pool.acquire() as conn:
            observed = await conn.fetchrow(
                "SELECT status,before_state FROM bp_release_jobs WHERE id=$1", prequery
            )
            assert observed["status"] == "waiting_confirmation"
            assert json.loads(observed["before_state"])["entry_type"] == "static"
            release = await conn.fetchval(
                """INSERT INTO bp_release_jobs(target_ip,kind,reason,status,phase,actor_id,
                actor_email,switch_id,command_profile_id,switch_version,command_profile_version,
                switch_name,switch_management_ip,preparation_job_id,before_state)
                SELECT target_ip,'release',reason,'queued','queued',actor_id,actor_email,switch_id,
                command_profile_id,switch_version,command_profile_version,switch_name,
                switch_management_ip,id,before_state
                FROM bp_release_jobs WHERE id=$1 RETURNING id""",
                prequery,
            )
        job = await claim(pool)
        assert job["id"] == release
        await execute(pool, job)
        async with pool.acquire() as conn:
            done = await conn.fetchrow(
                "SELECT status,after_state,raw_release_output FROM bp_release_jobs WHERE id=$1",
                release,
            )
            assert done["status"] == "succeeded", done
            assert json.loads(done["after_state"])["entry_type"] == "dynamic"
            assert fake.arp["10.20.30.40"][1] == "dynamic"

        server.close()
        await server.wait_closed()
        down = await run("reachability")
        assert down["status"] == "succeeded" and down["message"]
        async with pool.acquire() as conn:
            state = await conn.fetchrow(
                "SELECT reachable,reachability_error FROM bp_switches WHERE id=$1", switch
            )
            assert state["reachable"] is False
            assert (
                await conn.fetchval(
                    """SELECT count(*) FROM bp_alerts
                    WHERE switch_id=$1 AND kind='device_unreachable' AND status='open'""",
                    switch,
                )
                == 1
            )
    finally:
        server.close()
        await pool.close()
