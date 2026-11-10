"""Monitoring polls against a fake SNMP agent, through the real pysnmp client and the database."""

import asyncio
import base64
import json
import os
import random

import asyncpg
import pytest
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from pysnmp.hlapi.v3arch.asyncio import SnmpEngine
from worker import monitoring
from worker.devices import credential_key
from worker.fake_snmp import FakeSnmpAgent

PORT = 16161
HUAWEI = {
    "cpu": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.5"],
    "memory": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.7"],
    "temperature": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.11"],
}


def sealed(secret: str) -> str:
    nonce = os.urandom(12)
    payload = AESGCM(credential_key()).encrypt(nonce, secret.encode(), None)
    return "v1:" + base64.urlsafe_b64encode(nonce + payload).rstrip(b"=").decode()


@pytest.mark.asyncio
async def test_polls_store_readings_ports_rates_and_alerts(monkeypatch):
    dsn = os.getenv("EXECUTOR_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated test database required")
    monkeypatch.setattr(monitoring, "TIMEOUT", 0.3)
    monkeypatch.setattr(monitoring, "RETRIES", 0)
    key = credential_key()
    v2_ip = f"127.0.{random.randint(1, 254)}.{random.randint(2, 254)}"
    v3_ip = f"127.1.{random.randint(1, 254)}.{random.randint(2, 254)}"
    agent = FakeSnmpAgent(name="MON-FAKE", community="campus-ro")
    secure_agent = FakeSnmpAgent(
        name="MON-FAKE-V3", community="unused", v3_user=("monitor", "authpass1", "privpass1")
    )
    agent.start(v2_ip, PORT)
    secure_agent.start(v3_ip, PORT)
    pool = await asyncpg.create_pool(dsn, min_size=1, max_size=6)
    engine = SnmpEngine()
    try:
        async with pool.acquire() as conn:
            # Only this test's switches are monitored.
            await conn.execute("UPDATE bp_switches SET snmp_credential_id=NULL")
            await conn.execute("DELETE FROM bp_switch_monitoring")
            await conn.execute(
                """UPDATE bp_monitoring_settings SET interval_minutes=5,cpu_threshold=80,
                memory_threshold=90,temperature_threshold=70,consecutive_polls=2,alert_on_restart=true"""
            )
            login = await conn.fetchval(
                """INSERT INTO bp_credentials(name,username,encrypted_password)
                VALUES ($1,'netops',$2) RETURNING id""",
                f"mon-{v2_ip}",
                sealed("x"),
            )
            profile = await conn.fetchval(
                """INSERT INTO bp_command_profiles(name,vendor,snmp_rules) VALUES ($1,'huawei',$2)
                RETURNING id""",
                f"mon-{v2_ip}",
                json.dumps(HUAWEI),
            )
            v2 = await conn.fetchval(
                """INSERT INTO bp_snmp_credentials(name,snmp_version,encrypted_community)
                VALUES ($1,'v2c',$2) RETURNING id""",
                f"v2-{v2_ip}",
                sealed("campus-ro"),
            )
            v3 = await conn.fetchval(
                """INSERT INTO bp_snmp_credentials(name,snmp_version,username,auth_protocol,
                encrypted_auth_password,priv_protocol,encrypted_priv_password)
                VALUES ($1,'v3','monitor','sha',$2,'aes',$3) RETURNING id""",
                f"v3-{v2_ip}",
                sealed("authpass1"),
                sealed("privpass1"),
            )
            switch, secure = [
                await conn.fetchval(
                    """INSERT INTO bp_switches(name,management_ip,credential_id,command_profile_id,
                    vendor,snmp_credential_id,snmp_port) VALUES ($1,$2,$3,$4,'huawei',$5,$6)
                    RETURNING id""",
                    name,
                    ip,
                    login,
                    profile,
                    credential,
                    PORT,
                )
                for name, ip, credential in (("MON-1", v2_ip, v2), ("MON-3", v3_ip, v3))
            ]

        async def poll(*switches):
            async with pool.acquire() as conn:
                await conn.execute(
                    """UPDATE bp_switch_monitoring SET next_poll_at=now()
                    WHERE switch_id=ANY($1::uuid[])""",
                    list(switches),
                )
            claims = [
                c for c in await monitoring.claim_polls(pool, 10) if c["switch_id"] in switches
            ]
            assert len(claims) == len(switches)
            for claim in claims:
                await monitoring.poll_and_record(pool, engine, claim, key)

        async def state(switch_id):
            async with pool.acquire() as conn:
                return await conn.fetchrow(
                    "SELECT * FROM bp_switch_monitoring WHERE switch_id=$1", switch_id
                )

        async def ports(switch_id):
            async with pool.acquire() as conn:
                return {
                    row["if_index"]: row
                    for row in await conn.fetch(
                        "SELECT * FROM bp_switch_interfaces WHERE switch_id=$1", switch_id
                    )
                }

        async def alert(kind, switch_id):
            async with pool.acquire() as conn:
                return await conn.fetchval(
                    """SELECT message FROM bp_alerts
                    WHERE kind=$1 AND switch_id=$2 AND status<>'resolved'""",
                    kind,
                    switch_id,
                )

        # A switch never polled is due at once; the first poll reads everything but rates.
        await poll(switch, secure)
        first = await state(switch)
        assert (
            first["error"],
            first["sys_name"],
            first["cpu_percent"],
            first["memory_percent"],
            first["temperature_c"],
        ) == (
            None,
            "MON-FAKE",
            18,
            37,
            42,
        )
        assert (
            first["sys_object_id"] == "1.3.6.1.4.1.2011.2.23.469" and first["uptime_seconds"] >= 0
        )
        assert first["next_poll_at"] > first["polled_at"]
        assert (await state(secure))["sys_name"] == "MON-FAKE-V3"  # SNMP v3 with SHA and AES
        found = await ports(switch)
        assert sorted(found) == [1, 2, 3, 4]  # Vlanif10 and NULL0 are not ports
        assert (
            found[1]["alias"] == "上联核心"
            and found[1]["speed_mbps"] == 1000
            and found[1]["counter_bits"] == 64
        )
        assert found[4]["oper_up"] is False and found[1]["in_bps"] is None

        # The next poll works out rates from the counters.
        await asyncio.sleep(1.2)
        await poll(switch)
        uplink = (await ports(switch))[1]
        assert (
            9_000_000 < uplink["in_bps"] < 11_000_000 and 4_500_000 < uplink["out_bps"] < 5_500_000
        )
        assert uplink["new_errors"] == 0

        # CPU over its threshold for two polls raises an alert; two polls under it resolve it.
        agent.cpu = 95
        await poll(switch)
        assert await alert("cpu_high", switch) is None
        await poll(switch)
        assert await alert("cpu_high", switch) == "CPU at 95% for 2 polls (threshold 80%)"
        agent.cpu = 20
        await poll(switch)
        assert await alert("cpu_high", switch) is not None
        await poll(switch)
        assert await alert("cpu_high", switch) is None

        # A port going down is noted, and a restart raises an alert and skips the rates.
        agent.down = {2}
        agent.restart()
        await poll(switch)
        after = await state(switch)
        assert (
            after["restarted_at"] is not None
            and await alert("device_restarted", switch) == "Switch restarted"
        )
        found = await ports(switch)
        assert found[2]["oper_up"] is False and found[2]["oper_changed_at"] is not None
        assert found[4]["oper_up"] is True and found[1]["in_bps"] is None

        # A switch that stops answering (or refuses the community, which looks the same) opens an
        # alert after two polls in a row, and its readings from before stay. An answer resolves it.
        agent.stop()
        await poll(switch)
        failed = await state(switch)
        assert (
            failed["error"] == "No SNMP response received before timeout"
            and failed["failures"] == 1
        )
        assert failed["cpu_percent"] == 20 and await alert("snmp_unreachable", switch) is None
        await poll(switch)
        assert (await alert("snmp_unreachable", switch)).startswith("SNMP not responding")
        agent.start(v2_ip, PORT)
        await poll(switch)
        assert (await state(switch))["failures"] == 0 and await alert(
            "snmp_unreachable", switch
        ) is None

        async with pool.acquire() as conn:
            samples = await conn.fetchval(
                "SELECT count(*) FROM bp_switch_metrics WHERE switch_id=$1", switch
            )
            # Every successful poll keeps a sample: two failures among ten polls.
            assert samples == 8
            # A switch that stops being monitored while it is polled stores nothing from that poll.
            await conn.execute(
                "UPDATE bp_switch_monitoring SET next_poll_at=now() WHERE switch_id=$1", switch
            )
        [claim] = [c for c in await monitoring.claim_polls(pool, 10) if c["switch_id"] == switch]
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_switch_monitoring SET lease_token=gen_random_uuid() WHERE switch_id=$1",
                switch,
            )
        await monitoring.poll_and_record(pool, engine, claim, key)
        async with pool.acquire() as conn:
            assert (
                await conn.fetchval(
                    "SELECT count(*) FROM bp_switch_metrics WHERE switch_id=$1", switch
                )
                == 8
            )
    finally:
        agent.stop()
        secure_agent.stop()
        engine.close_dispatcher()
        await pool.close()
