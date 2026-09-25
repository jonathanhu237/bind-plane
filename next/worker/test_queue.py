import os

import asyncpg
import pytest
from bind_plane.security.credentials import decrypt_secret
from worker.main import checkpoint, claim, legacy_encrypted_secret, sweep_expired


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
    finally:
        await conn.close()


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
            id_ = await conn.fetchval(
                """INSERT INTO bp_release_jobs
                (target_ip,kind,reason,status,phase,actor_id,actor_email,switch_id,command_profile_id)
                VALUES('10.2.2.2','pre_release_query','temporary_test','queued','queued',$1,
                'operator@test.invalid',$2,$3) RETURNING id""",
                actor,
                switch["id"],
                switch["command_profile_id"],
            )
        job = await claim(pool)
        assert job["id"] == id_ and job["kind"] == "pre_release_query"
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
