import asyncio
from uuid import UUID

from bind_plane.api.deps import get_pre_release_enqueuer, get_release_enqueuer
from bind_plane.api.main import app
from bind_plane.db.models import (
    AuditLog,
    CommandProfile,
    Credential,
    Network,
    ReleaseJob,
    ReleaseJobEvent,
    ReleaseJobKind,
    ReleaseJobPhase,
    ReleaseJobStatus,
    RoleName,
    Switch,
    User,
    UserRole,
)
from bind_plane.security.passwords import hash_password
from fastapi.testclient import TestClient
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker
from sqlalchemy.orm import selectinload


async def _seed_environment(session_factory: async_sessionmaker[AsyncSession]) -> None:
    async with session_factory() as session:
        admin = User(
            username="admin",
            password_hash=hash_password("password123"),
            display_name="Admin",
            must_change_password=False,
        )
        admin.roles = [UserRole(role=RoleName.ADMIN)]
        operator = User(
            username="operator",
            password_hash=hash_password("password123"),
            display_name="Operator",
            must_change_password=False,
        )
        operator.roles = [UserRole(role=RoleName.OPERATOR)]
        credential = Credential(
            name="lab",
            username="netops",
            encrypted_password="encrypted",
        )
        profile = CommandProfile(
            name="h3c",
            command_templates={
                "single_arp_query": "display arp $ip",
                "arp_release": "undo arp static $ip",
            },
            prompt_patterns={},
            parser_rules={},
            error_patterns=["Error"],
        )
        session.add_all([admin, operator, credential, profile])
        await session.flush()
        switch = Switch(
            name="edge-sw-01",
            management_ip="10.0.0.10",
            credential_id=credential.id,
            command_profile_id=profile.id,
        )
        session.add(switch)
        await session.flush()
        session.add(
            Network(
                switch_id=switch.id,
                cidr="10.44.132.0/24",
                prefix_length=24,
                is_validated=True,
            )
        )
        await session.commit()


def _headers(client: TestClient, username: str) -> dict[str, str]:
    response = client.post(
        "/api/auth/login",
        json={"username": username, "password": "password123"},
    )
    assert response.status_code == 200
    return {"Authorization": f"Bearer {response.json()['access_token']}"}


async def _downgrade_admin_to_operator(
    session: AsyncSession,
) -> User:
    admin = await session.scalar(
        select(User).options(selectinload(User.roles)).where(User.username == "admin")
    )
    assert admin is not None
    admin.roles = [UserRole(role=RoleName.OPERATOR)]
    return admin


def test_authentication_and_admin_authorization(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    operator_headers = _headers(client, "operator")

    assert client.get("/api/auth/me", headers=operator_headers).status_code == 200
    response = client.get("/api/admin/users", headers=operator_headers)

    assert response.status_code == 403


def test_prepare_create_status_retry_and_audit_visibility(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    pre_query_queued: list[str] = []
    release_queued: list[str] = []
    app.dependency_overrides[get_pre_release_enqueuer] = lambda: (
        lambda job_id: pre_query_queued.append(str(job_id))
    )
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    operator_headers = _headers(client, "operator")
    admin_headers = _headers(client, "admin")

    prepare_response = client.post(
        "/api/releases/prepare",
        headers=operator_headers,
        json={"target_ip": "10.44.132.254", "reason": "temporary_test"},
    )
    assert prepare_response.status_code == 200
    assert prepare_response.json()["status"] == "query_queued"
    assert prepare_response.json()["resolved_switch"]["name"] == "edge-sw-01"
    preparation_job_id = prepare_response.json()["preparation_job_id"]
    assert pre_query_queued == [preparation_job_id]

    async def mark_waiting_confirmation() -> None:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(preparation_job_id))
            assert job is not None
            job.status = ReleaseJobStatus.WAITING_CONFIRMATION
            job.phase = ReleaseJobPhase.WAITING_CONFIRMATION
            job.before_state = {
                "target_ip": "10.44.132.254",
                "entry_type": "static",
                "mac": "0011-2233-4455",
            }
            job.raw_before_output = "10.44.132.254 0011-2233-4455 S"
            await session.commit()

    asyncio.run(mark_waiting_confirmation())

    create_response = client.post(
        "/api/releases/jobs",
        headers=operator_headers,
        json={
            "preparation_job_id": preparation_job_id,
            "target_ip": "10.44.132.254",
            "reason": "temporary_test",
            "ticket_id": "INC-1",
            "confirmed": True,
        },
    )
    assert create_response.status_code == 200
    job_id = create_response.json()["job_id"]
    assert job_id != preparation_job_id
    assert release_queued == [job_id]

    duplicate_create_response = client.post(
        "/api/releases/jobs",
        headers=operator_headers,
        json={
            "preparation_job_id": preparation_job_id,
            "target_ip": "10.44.132.254",
            "reason": "temporary_test",
            "ticket_id": "INC-1",
            "confirmed": True,
        },
    )
    assert duplicate_create_response.status_code == 200
    assert duplicate_create_response.json()["job_id"] == job_id
    assert release_queued == [job_id]

    mismatched_duplicate_response = client.post(
        "/api/releases/jobs",
        headers=operator_headers,
        json={
            "preparation_job_id": preparation_job_id,
            "target_ip": "10.44.132.254",
            "reason": "user_report",
            "ticket_id": "INC-1",
            "confirmed": True,
        },
    )
    assert mismatched_duplicate_response.status_code == 409
    assert release_queued == [job_id]

    detail_response = client.get(f"/api/releases/jobs/{job_id}", headers=operator_headers)
    assert detail_response.status_code == 200
    assert detail_response.json()["kind"] == "release"
    assert detail_response.json()["preparation_job_id"] == preparation_job_id
    assert detail_response.json()["phase"] == "queued"
    assert detail_response.json()["raw_output"] is None
    admin_detail_response = client.get(f"/api/releases/jobs/{job_id}", headers=admin_headers)
    assert admin_detail_response.status_code == 200
    assert admin_detail_response.json()["raw_output"]["before"]

    async def load_preparation_state() -> tuple[ReleaseJobStatus, ReleaseJobPhase, str | None]:
        async with session_factory() as session:
            preparation_job = await session.get(ReleaseJob, UUID(preparation_job_id))
            assert preparation_job is not None
            return (
                preparation_job.status,
                preparation_job.phase,
                preparation_job.result.get("release_job_id"),
            )

    assert asyncio.run(load_preparation_state()) == (
        ReleaseJobStatus.SUCCEEDED,
        ReleaseJobPhase.FINISHED,
        job_id,
    )

    async def mark_failed() -> None:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(job_id))
            assert job is not None
            job.status = ReleaseJobStatus.FAILED
            job.phase = ReleaseJobPhase.FINISHED
            await session.commit()

    asyncio.run(mark_failed())

    retry_response = client.post(f"/api/releases/jobs/{job_id}/retry", headers=operator_headers)
    assert retry_response.status_code == 200
    retry_id = retry_response.json()["job_id"]
    assert release_queued[-1] == retry_id

    async def load_retry_link() -> str | None:
        async with session_factory() as session:
            retry_job = await session.get(ReleaseJob, UUID(retry_id))
            assert retry_job is not None
            return str(retry_job.retry_of_id)

    assert asyncio.run(load_retry_link()) == job_id

    admin_audit = client.get("/api/audit", headers=admin_headers)
    assert admin_audit.status_code == 200
    assert len(admin_audit.json()) >= 1
    assert client.get("/api/audit", headers=operator_headers).status_code == 403


def test_prepare_marks_job_failed_when_enqueue_fails(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))

    def fail_enqueue(_: UUID) -> None:
        raise RuntimeError("queue unavailable")

    app.dependency_overrides[get_pre_release_enqueuer] = lambda: fail_enqueue
    operator_headers = _headers(client, "operator")

    response = client.post(
        "/api/releases/prepare",
        headers=operator_headers,
        json={"target_ip": "10.44.132.254", "reason": "temporary_test"},
    )

    assert response.status_code == 503
    job_id = response.json()["detail"]["job_id"]

    async def load_job() -> tuple[ReleaseJobStatus, ReleaseJobPhase, str | None]:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(job_id))
            assert job is not None
            return job.status, job.phase, job.error_message

    assert asyncio.run(load_job()) == (
        ReleaseJobStatus.FAILED,
        ReleaseJobPhase.FINISHED,
        "Queue enqueue failed",
    )


def test_create_marks_release_job_failed_when_enqueue_fails(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    pre_query_queued: list[str] = []
    app.dependency_overrides[get_pre_release_enqueuer] = lambda: (
        lambda job_id: pre_query_queued.append(str(job_id))
    )

    def fail_enqueue(_: UUID) -> None:
        raise RuntimeError("queue unavailable")

    app.dependency_overrides[get_release_enqueuer] = lambda: fail_enqueue
    operator_headers = _headers(client, "operator")

    prepare_response = client.post(
        "/api/releases/prepare",
        headers=operator_headers,
        json={"target_ip": "10.44.132.254", "reason": "temporary_test"},
    )
    preparation_job_id = prepare_response.json()["preparation_job_id"]

    async def mark_waiting_confirmation() -> None:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(preparation_job_id))
            assert job is not None
            job.status = ReleaseJobStatus.WAITING_CONFIRMATION
            job.phase = ReleaseJobPhase.WAITING_CONFIRMATION
            job.before_state = {"target_ip": "10.44.132.254", "entry_type": "static"}
            job.raw_before_output = "10.44.132.254 0011-2233-4455 S"
            await session.commit()

    asyncio.run(mark_waiting_confirmation())

    response = client.post(
        "/api/releases/jobs",
        headers=operator_headers,
        json={
            "preparation_job_id": preparation_job_id,
            "target_ip": "10.44.132.254",
            "reason": "temporary_test",
            "confirmed": True,
        },
    )

    assert response.status_code == 503
    release_job_id = response.json()["detail"]["job_id"]

    async def load_release_job() -> tuple[
        ReleaseJobStatus,
        ReleaseJobPhase,
        str | None,
        str | None,
    ]:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(release_job_id))
            assert job is not None
            return (
                job.status,
                job.phase,
                job.error_message,
                str(job.preparation_job_id),
            )

    assert asyncio.run(load_release_job()) == (
        ReleaseJobStatus.FAILED,
        ReleaseJobPhase.FINISHED,
        "Queue enqueue failed",
        preparation_job_id,
    )


def test_retry_marks_retry_job_failed_when_enqueue_fails(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))

    def fail_enqueue(_: UUID) -> None:
        raise RuntimeError("queue unavailable")

    app.dependency_overrides[get_release_enqueuer] = lambda: fail_enqueue
    operator_headers = _headers(client, "operator")

    async def add_failed_release() -> str:
        async with session_factory() as session:
            operator = await session.scalar(select(User).where(User.username == "operator"))
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            profile = await session.scalar(
                select(CommandProfile).where(CommandProfile.name == "h3c")
            )
            assert operator is not None
            assert switch is not None
            assert profile is not None
            job = ReleaseJob(
                target_ip="10.44.132.254",
                kind=ReleaseJobKind.RELEASE,
                reason="temporary_test",
                force=False,
                status=ReleaseJobStatus.FAILED,
                phase=ReleaseJobPhase.FINISHED,
                operator_id=operator.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
            )
            session.add(job)
            await session.commit()
            return str(job.id)

    original_id = asyncio.run(add_failed_release())

    response = client.post(f"/api/releases/jobs/{original_id}/retry", headers=operator_headers)

    assert response.status_code == 503
    retry_id = response.json()["detail"]["job_id"]

    async def load_retry_job() -> tuple[ReleaseJobStatus, ReleaseJobPhase, str | None, str | None]:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(retry_id))
            assert job is not None
            return job.status, job.phase, job.error_message, str(job.retry_of_id)

    assert asyncio.run(load_retry_job()) == (
        ReleaseJobStatus.FAILED,
        ReleaseJobPhase.FINISHED,
        "Queue enqueue failed",
        original_id,
    )


def test_retry_returns_existing_active_retry_without_duplicate_enqueue(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    release_queued: list[str] = []
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    operator_headers = _headers(client, "operator")

    async def add_failed_release() -> str:
        async with session_factory() as session:
            operator = await session.scalar(select(User).where(User.username == "operator"))
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            profile = await session.scalar(
                select(CommandProfile).where(CommandProfile.name == "h3c")
            )
            assert operator is not None
            assert switch is not None
            assert profile is not None
            job = ReleaseJob(
                target_ip="10.44.132.254",
                kind=ReleaseJobKind.RELEASE,
                reason="temporary_test",
                force=False,
                status=ReleaseJobStatus.FAILED,
                phase=ReleaseJobPhase.FINISHED,
                operator_id=operator.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
            )
            session.add(job)
            await session.commit()
            return str(job.id)

    original_id = asyncio.run(add_failed_release())

    first_response = client.post(
        f"/api/releases/jobs/{original_id}/retry",
        headers=operator_headers,
    )
    second_response = client.post(
        f"/api/releases/jobs/{original_id}/retry",
        headers=operator_headers,
    )

    assert first_response.status_code == 200
    assert second_response.status_code == 200
    assert second_response.json()["job_id"] == first_response.json()["job_id"]
    assert release_queued == [first_response.json()["job_id"]]


def test_duplicate_confirmation_reenqueues_existing_queued_release_without_confirmation_event(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    release_queued: list[str] = []
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    operator_headers = _headers(client, "operator")

    async def add_stranded_confirmed_release() -> tuple[str, str]:
        async with session_factory() as session:
            operator = await session.scalar(select(User).where(User.username == "operator"))
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            profile = await session.scalar(
                select(CommandProfile).where(CommandProfile.name == "h3c")
            )
            assert operator is not None
            assert switch is not None
            assert profile is not None
            preparation = ReleaseJob(
                target_ip="10.44.132.254",
                kind=ReleaseJobKind.PRE_RELEASE_QUERY,
                reason="temporary_test",
                force=False,
                status=ReleaseJobStatus.SUCCEEDED,
                phase=ReleaseJobPhase.FINISHED,
                before_state={"target_ip": "10.44.132.254", "entry_type": "static"},
                raw_before_output="10.44.132.254 0011-2233-4455 S",
                operator_id=operator.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
            )
            session.add(preparation)
            await session.flush()
            release = ReleaseJob(
                target_ip="10.44.132.254",
                kind=ReleaseJobKind.RELEASE,
                reason="temporary_test",
                force=False,
                status=ReleaseJobStatus.QUEUED,
                phase=ReleaseJobPhase.QUEUED,
                before_state=preparation.before_state,
                raw_before_output=preparation.raw_before_output,
                operator_id=operator.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
                preparation_job_id=preparation.id,
            )
            session.add(release)
            await session.flush()
            preparation.result = {"release_job_id": str(release.id)}
            await session.commit()
            return str(preparation.id), str(release.id)

    preparation_id, release_id = asyncio.run(add_stranded_confirmed_release())

    response = client.post(
        "/api/releases/jobs",
        headers=operator_headers,
        json={
            "preparation_job_id": preparation_id,
            "target_ip": "10.44.132.254",
            "reason": "temporary_test",
            "confirmed": True,
        },
    )

    assert response.status_code == 200
    assert response.json()["job_id"] == release_id
    assert release_queued == [release_id]

    async def has_enqueue_event() -> bool:
        async with session_factory() as session:
            event = await session.scalar(
                select(ReleaseJobEvent).where(
                    ReleaseJobEvent.job_id == UUID(release_id),
                    ReleaseJobEvent.message == "Queue enqueue confirmed",
                )
            )
            return event is not None

    assert asyncio.run(has_enqueue_event()) is True


def test_retry_reenqueues_existing_active_retry_without_confirmation_event(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    release_queued: list[str] = []
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    operator_headers = _headers(client, "operator")

    async def add_original_and_stranded_retry() -> tuple[str, str]:
        async with session_factory() as session:
            operator = await session.scalar(select(User).where(User.username == "operator"))
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            profile = await session.scalar(
                select(CommandProfile).where(CommandProfile.name == "h3c")
            )
            assert operator is not None
            assert switch is not None
            assert profile is not None
            original = ReleaseJob(
                target_ip="10.44.132.254",
                kind=ReleaseJobKind.RELEASE,
                reason="temporary_test",
                force=False,
                status=ReleaseJobStatus.FAILED,
                phase=ReleaseJobPhase.FINISHED,
                operator_id=operator.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
            )
            session.add(original)
            await session.flush()
            retry = ReleaseJob(
                target_ip=original.target_ip,
                kind=ReleaseJobKind.RELEASE,
                reason=original.reason,
                force=False,
                status=ReleaseJobStatus.QUEUED,
                phase=ReleaseJobPhase.QUEUED,
                operator_id=operator.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
                retry_of_id=original.id,
            )
            session.add(retry)
            await session.commit()
            return str(original.id), str(retry.id)

    original_id, retry_id = asyncio.run(add_original_and_stranded_retry())

    response = client.post(f"/api/releases/jobs/{original_id}/retry", headers=operator_headers)

    assert response.status_code == 200
    assert response.json()["job_id"] == retry_id
    assert release_queued == [retry_id]


def test_former_admin_cannot_retry_forced_release_after_role_downgrade(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    release_queued: list[str] = []
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    admin_headers = _headers(client, "admin")

    async def add_failed_forced_release_and_downgrade() -> str:
        async with session_factory() as session:
            admin = await _downgrade_admin_to_operator(session)
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            profile = await session.scalar(
                select(CommandProfile).where(CommandProfile.name == "h3c")
            )
            assert switch is not None
            assert profile is not None
            job = ReleaseJob(
                target_ip="10.44.132.254",
                kind=ReleaseJobKind.RELEASE,
                reason="temporary_test",
                force=True,
                status=ReleaseJobStatus.FAILED,
                phase=ReleaseJobPhase.FINISHED,
                operator_id=admin.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
            )
            session.add(job)
            await session.commit()
            return str(job.id)

    original_id = asyncio.run(add_failed_forced_release_and_downgrade())

    response = client.post(f"/api/releases/jobs/{original_id}/retry", headers=admin_headers)

    assert response.status_code == 403
    assert response.json()["detail"] == "Only admin can retry forced release jobs"
    assert release_queued == []


def test_forced_release_is_admin_only_when_pre_query_is_missing(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    pre_query_queued: list[str] = []
    app.dependency_overrides[get_pre_release_enqueuer] = lambda: (
        lambda job_id: pre_query_queued.append(str(job_id))
    )
    operator_headers = _headers(client, "operator")
    admin_headers = _headers(client, "admin")

    operator_response = client.post(
        "/api/releases/prepare",
        headers=operator_headers,
        json={"target_ip": "10.44.132.254", "reason": "temporary_test", "force": True},
    )
    admin_response = client.post(
        "/api/releases/prepare",
        headers=admin_headers,
        json={"target_ip": "10.44.132.254", "reason": "temporary_test", "force": True},
    )

    assert operator_response.status_code == 403
    assert admin_response.status_code == 200
    assert admin_response.json()["status"] == "query_queued"
    assert admin_response.json()["force"] is True
    assert pre_query_queued == [admin_response.json()["preparation_job_id"]]


def test_admin_can_select_switch_for_forced_prepare_when_no_network_matches(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    pre_query_queued: list[str] = []
    release_queued: list[str] = []
    app.dependency_overrides[get_pre_release_enqueuer] = lambda: (
        lambda job_id: pre_query_queued.append(str(job_id))
    )
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    admin_headers = _headers(client, "admin")

    async def load_switch_id() -> str:
        async with session_factory() as session:
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            assert switch is not None
            return str(switch.id)

    switch_id = asyncio.run(load_switch_id())
    prepare_response = client.post(
        "/api/releases/prepare",
        headers=admin_headers,
        json={
            "target_ip": "10.55.1.10",
            "reason": "temporary_test",
            "force": True,
            "selected_switch_id": switch_id,
        },
    )

    assert prepare_response.status_code == 200
    body = prepare_response.json()
    assert body["status"] == "query_queued"
    assert body["force"] is True
    assert body["resolved_switch"]["switch_id"] == switch_id
    assert body["resolved_switch"]["network_id"] is None
    assert body["resolved_switch"]["cidr"] is None
    assert body["resolved_switch"]["selection_source"] == "selected_switch"
    assert pre_query_queued == [body["preparation_job_id"]]

    preparation_job_id = body["preparation_job_id"]

    async def mark_waiting_confirmation_without_result_context() -> None:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(preparation_job_id))
            assert job is not None
            job.status = ReleaseJobStatus.WAITING_CONFIRMATION
            job.phase = ReleaseJobPhase.WAITING_CONFIRMATION
            job.before_state = {
                "target_ip": "10.55.1.10",
                "entry_type": "missing",
                "mac": None,
            }
            job.raw_before_output = "No matching ARP entry"
            job.result = {
                "preparation_status": "ready",
                "message": "Pre-release query complete; waiting for confirmation",
                "before_state": job.before_state,
            }
            await session.commit()

    asyncio.run(mark_waiting_confirmation_without_result_context())

    create_response = client.post(
        "/api/releases/jobs",
        headers=admin_headers,
        json={
            "preparation_job_id": preparation_job_id,
            "target_ip": "10.55.1.10",
            "reason": "temporary_test",
            "force": True,
            "confirmed": True,
        },
    )

    assert create_response.status_code == 200
    release_job_id = create_response.json()["job_id"]
    assert release_queued == [release_job_id]

    async def load_release_context() -> tuple[dict, str | None, str | None]:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(release_job_id))
            assert job is not None
            created_audit = await session.scalar(
                select(AuditLog).where(
                    AuditLog.target_id == job.id,
                    AuditLog.action == "release_job_created",
                )
            )
            assert created_audit is not None
            return (
                job.result,
                created_audit.payload.get("switch_selection"),
                created_audit.payload.get("selected_switch_id"),
            )

    assert asyncio.run(load_release_context()) == (
        {"switch_selection": "selected_switch", "selected_switch_id": switch_id},
        "selected_switch",
        switch_id,
    )


def test_selected_switch_forced_prepare_rejects_unusable_switch(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    pre_query_queued: list[str] = []
    app.dependency_overrides[get_pre_release_enqueuer] = lambda: (
        lambda job_id: pre_query_queued.append(str(job_id))
    )
    admin_headers = _headers(client, "admin")

    async def disable_switch() -> str:
        async with session_factory() as session:
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            assert switch is not None
            switch.is_enabled = False
            await session.commit()
            return str(switch.id)

    switch_id = asyncio.run(disable_switch())

    response = client.post(
        "/api/releases/prepare",
        headers=admin_headers,
        json={
            "target_ip": "10.55.1.10",
            "reason": "temporary_test",
            "force": True,
            "selected_switch_id": switch_id,
        },
    )

    assert response.status_code == 409
    assert response.json()["detail"] == "Selected switch is not enabled"
    assert pre_query_queued == []


def test_admin_can_force_after_normal_pre_query_finds_no_record(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    pre_query_queued: list[str] = []
    release_queued: list[str] = []
    app.dependency_overrides[get_pre_release_enqueuer] = lambda: (
        lambda job_id: pre_query_queued.append(str(job_id))
    )
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    admin_headers = _headers(client, "admin")

    prepare_response = client.post(
        "/api/releases/prepare",
        headers=admin_headers,
        json={"target_ip": "10.44.132.254", "reason": "temporary_test"},
    )
    assert prepare_response.status_code == 200
    preparation_job_id = prepare_response.json()["preparation_job_id"]

    async def mark_no_record() -> None:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(preparation_job_id))
            assert job is not None
            job.status = ReleaseJobStatus.CANCELLED
            job.phase = ReleaseJobPhase.FINISHED
            job.before_state = {
                "target_ip": "10.44.132.254",
                "entry_type": "missing",
                "mac": None,
            }
            job.raw_before_output = "No matching ARP entry"
            job.result = {
                "preparation_status": "stopped_no_record",
                "message": "No pre-release record was found; normal release stopped",
                "before_state": job.before_state,
            }
            await session.commit()

    asyncio.run(mark_no_record())

    response = client.post(
        "/api/releases/jobs",
        headers=admin_headers,
        json={
            "preparation_job_id": preparation_job_id,
            "target_ip": "10.44.132.254",
            "reason": "temporary_test",
            "force": True,
            "confirmed": True,
        },
    )

    assert response.status_code == 200
    release_job_id = response.json()["job_id"]
    assert release_queued == [release_job_id]

    async def load_release_and_audit() -> tuple[bool, dict, str | None, bool | None, bool | None]:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(release_job_id))
            assert job is not None
            created_audit = await session.scalar(
                select(AuditLog).where(
                    AuditLog.target_id == job.id,
                    AuditLog.action == "release_job_created",
                )
            )
            pre_query_audit = await session.scalar(
                select(AuditLog).where(
                    AuditLog.target_id == UUID(preparation_job_id),
                    AuditLog.action == "release_pre_query_confirmed",
                )
            )
            assert created_audit is not None
            assert pre_query_audit is not None
            return (
                job.force,
                job.before_state,
                job.raw_before_output,
                created_audit.payload.get("force"),
                pre_query_audit.payload.get("force"),
            )

    assert asyncio.run(load_release_and_audit()) == (
        True,
        {"target_ip": "10.44.132.254", "entry_type": "missing", "mac": None},
        "No matching ARP entry",
        True,
        True,
    )


def test_former_admin_cannot_confirm_forced_pre_query_after_role_downgrade(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    release_queued: list[str] = []
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    admin_headers = _headers(client, "admin")

    async def add_waiting_forced_pre_query_and_downgrade() -> str:
        async with session_factory() as session:
            admin = await _downgrade_admin_to_operator(session)
            switch = await session.scalar(select(Switch).where(Switch.name == "edge-sw-01"))
            profile = await session.scalar(
                select(CommandProfile).where(CommandProfile.name == "h3c")
            )
            assert switch is not None
            assert profile is not None
            job = ReleaseJob(
                target_ip="10.44.132.254",
                kind=ReleaseJobKind.PRE_RELEASE_QUERY,
                reason="temporary_test",
                force=True,
                status=ReleaseJobStatus.WAITING_CONFIRMATION,
                phase=ReleaseJobPhase.WAITING_CONFIRMATION,
                before_state={
                    "target_ip": "10.44.132.254",
                    "entry_type": "missing",
                    "mac": None,
                },
                raw_before_output="No matching ARP entry",
                result={
                    "preparation_status": "ready",
                    "message": "Forced pre-release query complete; waiting for confirmation",
                },
                operator_id=admin.id,
                switch_id=switch.id,
                command_profile_id=profile.id,
            )
            session.add(job)
            await session.commit()
            return str(job.id)

    preparation_job_id = asyncio.run(add_waiting_forced_pre_query_and_downgrade())

    response = client.post(
        "/api/releases/jobs",
        headers=admin_headers,
        json={
            "preparation_job_id": preparation_job_id,
            "target_ip": "10.44.132.254",
            "reason": "temporary_test",
            "force": True,
            "confirmed": True,
        },
    )

    assert response.status_code == 403
    assert response.json()["detail"] == "Only admin can force release jobs"
    assert release_queued == []


def test_failed_pre_release_query_cannot_be_retried_as_release(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))
    pre_query_queued: list[str] = []
    release_queued: list[str] = []
    app.dependency_overrides[get_pre_release_enqueuer] = lambda: (
        lambda job_id: pre_query_queued.append(str(job_id))
    )
    app.dependency_overrides[get_release_enqueuer] = lambda: (
        lambda job_id: release_queued.append(str(job_id))
    )
    operator_headers = _headers(client, "operator")

    prepare_response = client.post(
        "/api/releases/prepare",
        headers=operator_headers,
        json={"target_ip": "10.44.132.254", "reason": "temporary_test"},
    )
    assert prepare_response.status_code == 200
    preparation_job_id = prepare_response.json()["preparation_job_id"]

    async def mark_pre_query_failed() -> None:
        async with session_factory() as session:
            job = await session.get(ReleaseJob, UUID(preparation_job_id))
            assert job is not None
            assert job.kind == ReleaseJobKind.PRE_RELEASE_QUERY
            job.status = ReleaseJobStatus.FAILED
            job.phase = ReleaseJobPhase.FINISHED
            await session.commit()

    asyncio.run(mark_pre_query_failed())

    retry_response = client.post(
        f"/api/releases/jobs/{preparation_job_id}/retry",
        headers=operator_headers,
    )

    assert retry_response.status_code == 409
    assert release_queued == []


def test_operator_cannot_access_audit_logs(
    client: TestClient,
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    asyncio.run(_seed_environment(session_factory))

    async def add_admin_audit() -> None:
        async with session_factory() as session:
            admin = await session.scalar(select(User).where(User.username == "admin"))
            assert admin is not None
            session.add(
                AuditLog(
                    actor_id=admin.id,
                    action="admin_only",
                    target_type="test",
                    payload={},
                )
            )
            await session.commit()

    asyncio.run(add_admin_audit())
    operator_headers = _headers(client, "operator")

    response = client.get("/api/audit", headers=operator_headers)

    assert response.status_code == 403
