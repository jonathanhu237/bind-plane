import asyncio
import os

import asyncpg
import pytest
from bind_plane.security.credentials import decrypt_secret
from worker.main import checkpoint, claim, legacy_encrypted_secret, load_dependencies, sweep_expired


async def queue_job(conn, actor, switch_id, ip, *, kind="release", status="queued", phase="queued"):
    """Insert a job pinned to the switch's current revision, as the API does."""
    return await conn.fetchval(
        """INSERT INTO bp_release_jobs
        (target_ip,kind,reason,status,phase,actor_id,actor_email,switch_id,command_profile_id,
         switch_version,command_profile_version,switch_name,switch_management_ip)
        SELECT $1::inet,$2::text,'temporary_test',$3::text,$4::text,$5::uuid,
         'operator@test.invalid',s.id,s.command_profile_id,s.version,p.version,s.name,
         s.management_ip
        FROM bp_switches s JOIN bp_command_profiles p ON p.id=s.command_profile_id
        WHERE s.id=$6 RETURNING id""",
        ip,
        kind,
        status,
        phase,
        actor,
        switch_id,
    )


@pytest.mark.asyncio
async def test_go_encrypted_credential_is_readable_only_with_worker_key():
    dsn = os.getenv("EXECUTOR_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated test database required")
    conn = await asyncpg.connect(dsn)
    try:
        encrypted = await conn.fetchval(
            "SELECT encrypted_password FROM bp_credentials WHERE name='audit-test'"
        )
        assert encrypted.startswith("v1:")
        assert decrypt_secret(legacy_encrypted_secret(encrypted)) == "unique-switch-secret"
        switch = await conn.fetchrow(
            "SELECT id,command_profile_id FROM bp_switches WHERE management_ip='192.0.2.1'"
        )
        pool = await asyncpg.create_pool(dsn, min_size=1, max_size=1)
        try:
            dependencies = await load_dependencies(
                pool,
                {"switch_id": switch["id"], "command_profile_id": switch["command_profile_id"]},
            )
            assert dependencies["management_ip"] == "192.0.2.1"
            assert dependencies["switch_version"] >= 1
        finally:
            await pool.close()
    finally:
        await conn.close()


@pytest.mark.asyncio
async def test_release_for_same_ip_waits_for_running_and_uncertain_command():
    dsn = os.getenv("EXECUTOR_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated test database required")
    pool = await asyncpg.create_pool(dsn, min_size=1, max_size=2)
    try:
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_release_jobs SET status='cancelled' WHERE status='queued'"
            )
            actor = await conn.fetchval("SELECT id FROM auth_users LIMIT 1")
            switch = await conn.fetchrow("SELECT id,command_profile_id FROM bp_switches LIMIT 1")
            assert actor is not None and switch is not None
            ids = []
            for _ in range(2):
                ids.append(await queue_job(conn, actor, switch["id"], "10.8.8.8"))
        first = await claim(pool)
        assert first["id"] == ids[0]
        assert await claim(pool) is None
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_release_jobs SET lease_until=now()-interval '1 second' WHERE id=$1",
                first["id"],
            )
        await sweep_expired(pool)
        assert await claim(pool) is None
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_release_jobs SET status='failed' WHERE id=$1", first["id"]
            )
        second = await claim(pool)
        assert second["id"] == ids[1]
        await checkpoint(pool, second, phase="finished", status="cancelled")
    finally:
        await pool.close()


@pytest.mark.asyncio
async def test_claim_rechecks_manual_barrier_after_waiting_for_ip_lock():
    dsn = os.getenv("EXECUTOR_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated test database required")
    pool = await asyncpg.create_pool(dsn, min_size=1, max_size=3)
    try:
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_release_jobs SET status='cancelled' WHERE status='queued'"
            )
            actor = await conn.fetchval("SELECT id FROM auth_users LIMIT 1")
            switch = await conn.fetchrow("SELECT id,command_profile_id FROM bp_switches LIMIT 1")
            assert actor is not None and switch is not None
            queued = await queue_job(conn, actor, switch["id"], "10.8.8.9")
            async with conn.transaction():
                await conn.execute("SELECT pg_advisory_xact_lock(hashtextextended('10.8.8.9',0))")
                pending = asyncio.create_task(claim(pool))
                await asyncio.sleep(0.1)
                assert not pending.done()
                await queue_job(
                    conn,
                    actor,
                    switch["id"],
                    "10.8.8.9",
                    status="needs_manual_confirmation",
                    phase="finished",
                )
        assert await asyncio.wait_for(pending, timeout=3) is None
        async with pool.acquire() as conn:
            assert (
                await conn.fetchval("SELECT status FROM bp_release_jobs WHERE id=$1", queued)
                == "queued"
            )
    finally:
        await pool.close()


@pytest.mark.asyncio
async def test_claim_checkpoint_and_expired_lease_never_requeue():
    dsn = os.getenv("EXECUTOR_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated test database required")
    pool = await asyncpg.create_pool(dsn, min_size=1, max_size=2)
    try:
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_release_jobs SET status='cancelled' WHERE status='queued'"
            )
            actor = await conn.fetchval("SELECT id FROM auth_users LIMIT 1")
            switch = await conn.fetchrow("SELECT id,command_profile_id FROM bp_switches LIMIT 1")
            assert actor is not None and switch is not None
            id_ = await queue_job(conn, actor, switch["id"], "10.2.2.2", kind="pre_release_query")
        job = await claim(pool)
        assert job["id"] == id_ and job["kind"] == "pre_release_query"
        assert job["target_ip"] == "10.2.2.2"
        await checkpoint(
            pool,
            job,
            phase="querying_before",
            before_state={"entry_type": "static"},
            raw_before_output="10.2.2.2 aa:bb static",
        )
        async with pool.acquire() as conn:
            await conn.execute(
                "UPDATE bp_release_jobs SET lease_until=now()-interval '1 second' WHERE id=$1", id_
            )
        await sweep_expired(pool)
        async with pool.acquire() as conn:
            row = await conn.fetchrow(
                "SELECT status,raw_before_output FROM bp_release_jobs WHERE id=$1", id_
            )
            assert row["status"] == "needs_manual_confirmation"
            assert row["raw_before_output"] == "10.2.2.2 aa:bb static"
        assert await claim(pool) is None
        with pytest.raises(RuntimeError, match="lost execution lease"):
            await checkpoint(pool, job, phase="releasing")
    finally:
        await pool.close()
