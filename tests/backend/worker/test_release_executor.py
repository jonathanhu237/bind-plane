from collections.abc import Callable

import pytest
from bind_plane.db.models import (
    AuditLog,
    CommandProfile,
    Credential,
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
from bind_plane.security.credentials import encrypt_secret
from bind_plane.security.passwords import hash_password
from bind_plane.services.command_profiles import RenderedCommands
from bind_plane.worker.release_executor import (
    execute_pre_release_query_job,
    execute_release_job,
    update_job_phase,
)
from bind_plane.worker.sessions import (
    NetmikoConnectionSettings,
    SwitchConnectionTimeoutError,
    SwitchSessionError,
)
from rq.timeouts import JobTimeoutException
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker


def _session_factory(
    *,
    before_output: str = "10.44.132.254 0011-2233-4455 S",
    release_output: str = "undo arp static 10.44.132.254",
    after_output: str = "10.44.132.254 0011-2233-4455 D",
    calls: list[str] | None = None,
    connect_error: Exception | None = None,
    query_before_error: Exception | None = None,
    release_error: Exception | None = None,
    query_after_error: Exception | None = None,
    close_error: Exception | None = None,
) -> Callable[[NetmikoConnectionSettings], object]:
    class FakeSession:
        def __init__(self, settings: NetmikoConnectionSettings) -> None:
            self.settings = settings
            self.closed = False

        def connect(self) -> None:
            if calls is not None:
                calls.append("connect")
            if connect_error is not None:
                raise connect_error

        def query_before(self, commands: RenderedCommands) -> str:
            if calls is not None:
                calls.append("query_before")
            if query_before_error is not None:
                raise query_before_error
            return before_output

        def release(self, commands: RenderedCommands) -> str:
            if calls is not None:
                calls.append("release")
            if release_error is not None:
                raise release_error
            return release_output

        def query_after(self, commands: RenderedCommands) -> str:
            if calls is not None:
                calls.append("query_after")
            if query_after_error is not None:
                raise query_after_error
            return after_output

        def close(self) -> None:
            if calls is not None:
                calls.append("close")
            if close_error is not None:
                raise close_error
            self.closed = True

    return FakeSession


async def _seed_job(
    session: AsyncSession,
    *,
    force: bool = False,
    kind: ReleaseJobKind = ReleaseJobKind.RELEASE,
    command_templates: dict | None = None,
    login_prompt_patterns: dict | None = None,
    prompt_patterns: dict | None = None,
    parser_rules: dict | None = None,
    error_patterns: list[str] | None = None,
    success_patterns: list[str] | None = None,
    encrypted_password: str | None = None,
    credential_active: bool = True,
    profile_active: bool = True,
    switch_enabled: bool = True,
) -> ReleaseJob:
    operator = User(
        username="operator",
        password_hash=hash_password("password123"),
        must_change_password=False,
    )
    operator.roles = [UserRole(role=RoleName.OPERATOR)]
    credential = Credential(
        name="lab",
        username="netops",
        encrypted_password=encrypted_password or encrypt_secret("switch-password"),
        is_active=credential_active,
    )
    profile = CommandProfile(
        name="h3c",
        command_templates=command_templates
        if command_templates is not None
        else {
            "single_arp_query": "display arp $ip",
            "arp_release": "undo arp static $ip",
        },
        login_prompt_patterns=login_prompt_patterns or {},
        prompt_patterns=prompt_patterns
        if prompt_patterns is not None
        else {"connection_options": {"device_type": "hp_comware_telnet"}},
        parser_rules=parser_rules
        if parser_rules is not None
        else {
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
            "missing_patterns": ["No matching"],
        },
        error_patterns=error_patterns if error_patterns is not None else ["Error"],
        success_patterns=success_patterns if success_patterns is not None else [],
        is_active=profile_active,
    )
    session.add_all([operator, credential, profile])
    await session.flush()
    switch = Switch(
        name="edge-sw-01",
        management_ip="10.0.0.10",
        credential_id=credential.id,
        command_profile_id=profile.id,
        is_enabled=switch_enabled,
    )
    session.add(switch)
    await session.flush()
    job = ReleaseJob(
        target_ip="10.44.132.254",
        kind=kind,
        reason="temporary_test",
        force=force,
        status=ReleaseJobStatus.QUEUED,
        phase=ReleaseJobPhase.QUEUED,
        operator_id=operator.id,
        switch_id=switch.id,
        command_profile_id=profile.id,
    )
    session.add(job)
    await session.commit()
    return job


async def test_execute_release_job_succeeds_when_dynamic_entry_remains(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

        audit = await session.scalar(select(AuditLog).where(AuditLog.target_id == job.id))

    assert executed.status == ReleaseJobStatus.SUCCEEDED
    assert executed.after_state["entry_type"] == "dynamic"
    assert executed.raw_before_output
    assert audit is not None
    assert audit.payload["started_at"]
    assert audit.payload["finished_at"]
    assert audit.payload["raw_after_output"]


async def test_execute_release_job_skips_non_queued_jobs(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)
        job.status = ReleaseJobStatus.SUCCEEDED
        job.phase = ReleaseJobPhase.FINISHED
        await session.commit()

        def fail_if_session_created(_: NetmikoConnectionSettings) -> object:
            raise AssertionError("worker should not open a session for non-queued jobs")

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=fail_if_session_created,
        )
        events = (
            await session.scalars(
                select(ReleaseJobEvent).where(ReleaseJobEvent.job_id == job.id)
            )
        ).all()

    assert executed.status == ReleaseJobStatus.SUCCEEDED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert events == []


async def test_execute_release_job_persists_terminal_state_when_close_fails(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(close_error=RuntimeError("disconnect failed")),
        )

        audit = await session.scalar(select(AuditLog).where(AuditLog.target_id == job.id))
        terminal_event = await session.scalar(
            select(ReleaseJobEvent)
            .where(
                ReleaseJobEvent.job_id == job.id,
                ReleaseJobEvent.phase == ReleaseJobPhase.FINISHED,
            )
            .order_by(ReleaseJobEvent.created_at.desc())
        )

    assert executed.status == ReleaseJobStatus.SUCCEEDED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.result["close_error"] == "Switch session close failed: RuntimeError"
    assert audit is not None
    assert audit.payload["result"]["close_error"] == "Switch session close failed: RuntimeError"
    assert terminal_event is not None
    assert terminal_event.payload["close_error"] == "Switch session close failed: RuntimeError"


async def test_update_job_phase_commits_visible_checkpoint(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)
        await update_job_phase(session, job, phase=ReleaseJobPhase.RELEASING)

        async with session_factory() as observer_session:
            persisted = await observer_session.get(ReleaseJob, job.id)

    assert persisted is not None
    assert persisted.status == ReleaseJobStatus.RUNNING
    assert persisted.phase == ReleaseJobPhase.RELEASING


async def test_execute_release_job_persists_partial_release_output_on_session_error(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(
                release_error=SwitchSessionError(
                    "Switch command failed",
                    partial_output="system-view\npartial release transcript",
                )
            ),
        )

        audit = await session.scalar(select(AuditLog).where(AuditLog.target_id == job.id))

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.raw_release_output == "system-view\npartial release transcript"
    assert audit is not None
    assert audit.payload["raw_release_output"] == "system-view\npartial release transcript"
    assert audit.payload["error_message"] == "Switch command failed"


@pytest.mark.parametrize(
    ("before_output", "expected_status", "expected_message"),
    [
        (
            "No matching ARP entry",
            ReleaseJobStatus.CANCELLED,
            "No pre-release record was found; release stopped before command",
        ),
        (
            "unstructured output",
            ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION,
            "Pre-release state could not be parsed confidently",
        ),
    ],
)
async def test_execute_release_job_stops_before_release_when_before_state_is_not_actionable(
    session_factory: async_sessionmaker[AsyncSession],
    before_output: str,
    expected_status: ReleaseJobStatus,
    expected_message: str,
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)
        calls: list[str] = []

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(before_output=before_output, calls=calls),
        )

    assert executed.status == expected_status
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.result["message"] == expected_message
    assert calls == ["connect", "query_before", "close"]


async def test_execute_pre_release_query_waits_for_confirmation_when_record_exists(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, kind=ReleaseJobKind.PRE_RELEASE_QUERY)

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

        audit = await session.scalar(
            select(AuditLog).where(
                AuditLog.target_id == job.id,
                AuditLog.action == "release_pre_query_completed",
            )
        )

    assert prepared.status == ReleaseJobStatus.WAITING_CONFIRMATION
    assert prepared.phase == ReleaseJobPhase.WAITING_CONFIRMATION
    assert prepared.before_state["entry_type"] == "static"
    assert prepared.raw_before_output
    assert audit is not None
    assert audit.payload["started_at"]
    assert "finished_at" in audit.payload
    assert audit.payload["raw_before_output"]


async def test_execute_pre_release_query_stops_normal_release_when_record_missing(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, kind=ReleaseJobKind.PRE_RELEASE_QUERY)

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(before_output="No matching ARP entry"),
        )

    assert prepared.status == ReleaseJobStatus.CANCELLED
    assert prepared.phase == ReleaseJobPhase.FINISHED
    assert prepared.result["preparation_status"] == "stopped_no_record"


async def test_execute_pre_release_query_allows_forced_missing_record(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, force=True, kind=ReleaseJobKind.PRE_RELEASE_QUERY)

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(before_output="No matching ARP entry"),
        )

    assert prepared.status == ReleaseJobStatus.WAITING_CONFIRMATION
    assert prepared.phase == ReleaseJobPhase.WAITING_CONFIRMATION
    assert prepared.before_state["entry_type"] == "missing"


async def test_execute_pre_release_query_blocks_unknown_state(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, kind=ReleaseJobKind.PRE_RELEASE_QUERY)

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(before_output="unstructured output"),
        )

    assert prepared.status == ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION
    assert prepared.phase == ReleaseJobPhase.FINISHED
    assert prepared.result["preparation_status"] == "needs_manual_confirmation"


async def test_execute_pre_release_query_clears_force_when_record_exists(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, force=True, kind=ReleaseJobKind.PRE_RELEASE_QUERY)

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert prepared.status == ReleaseJobStatus.WAITING_CONFIRMATION
    assert prepared.force is False


@pytest.mark.parametrize(
    ("release_output", "after_output", "expected_status"),
    [
        ("Error: invalid ARP entry", "10.44.132.254 0011-2233-4455 D", ReleaseJobStatus.FAILED),
        (
            "undo arp static 10.44.132.254",
            "10.44.132.254 0011-2233-4455 S",
            ReleaseJobStatus.FAILED,
        ),
        (
            "undo arp static 10.44.132.254",
            "10.44.132.254 0011-2233-4455 strange",
            ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION,
        ),
    ],
)
async def test_execute_release_job_classifies_failure_and_unknown_paths(
    session_factory: async_sessionmaker[AsyncSession],
    release_output: str,
    after_output: str,
    expected_status: ReleaseJobStatus,
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(
                release_output=release_output,
                after_output=after_output,
            ),
        )

    assert executed.status == expected_status
    assert executed.phase == ReleaseJobPhase.FINISHED


async def test_execute_release_job_marks_timeout(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(
                connect_error=SwitchConnectionTimeoutError("Switch connection timed out")
            ),
        )

    assert executed.status == ReleaseJobStatus.TIMEOUT
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "Switch connection timed out"


@pytest.mark.parametrize(
    "session_kwargs",
    [
        {"connect_error": JobTimeoutException("RQ job timed out")},
        {"query_before_error": JobTimeoutException("RQ job timed out")},
        {"release_error": JobTimeoutException("RQ job timed out")},
        {"query_after_error": JobTimeoutException("RQ job timed out")},
        {"close_error": JobTimeoutException("RQ job timed out")},
    ],
)
async def test_execute_release_job_marks_rq_timeout(
    session_factory: async_sessionmaker[AsyncSession],
    session_kwargs: dict,
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(**session_kwargs),
        )

    assert executed.status == ReleaseJobStatus.TIMEOUT
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "RQ job timed out"
    assert "close_error" not in executed.result


@pytest.mark.parametrize(
    "session_kwargs",
    [
        {"connect_error": JobTimeoutException("RQ pre-release timeout")},
        {"query_before_error": JobTimeoutException("RQ pre-release timeout")},
        {"close_error": JobTimeoutException("RQ pre-release timeout")},
    ],
)
async def test_execute_pre_release_query_marks_rq_timeout(
    session_factory: async_sessionmaker[AsyncSession],
    session_kwargs: dict,
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, kind=ReleaseJobKind.PRE_RELEASE_QUERY)

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(**session_kwargs),
        )

    assert prepared.status == ReleaseJobStatus.TIMEOUT
    assert prepared.phase == ReleaseJobPhase.FINISHED
    assert prepared.error_message == "RQ pre-release timeout"
    assert "close_error" not in prepared.result


async def test_execute_release_job_marks_bad_credential_decrypt_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, encrypted_password="not-fernet-token")

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "Switch credential cannot be decrypted"


async def test_execute_release_job_marks_bad_connection_options_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(
            session,
            prompt_patterns={"connection_options": {"port": "bad-port"}},
        )

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "Invalid connection option: port"


async def test_execute_pre_release_query_marks_unsupported_prompt_pattern_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(
            session,
            kind=ReleaseJobKind.PRE_RELEASE_QUERY,
            prompt_patterns={"unexpected_prompt_rule": "#"},
        )

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert prepared.status == ReleaseJobStatus.FAILED
    assert prepared.phase == ReleaseJobPhase.FINISHED
    assert prepared.error_message == "Unsupported prompt pattern(s): unexpected_prompt_rule"


async def test_execute_release_job_marks_missing_command_profile_template_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(
            session,
            command_templates={"single_arp_query": "display arp $ip"},
        )

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "Missing command template: arp_release"


async def test_execute_release_job_rejects_unknown_template_placeholder_before_session_opens(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(
            session,
            command_templates={
                "single_arp_query": "display arp $ip vpn $vpn",
                "arp_release": "undo arp static $ip",
            },
        )
        session_opened = False

        def fail_if_session_created(_: NetmikoConnectionSettings) -> object:
            nonlocal session_opened
            session_opened = True
            raise AssertionError("worker should reject bad templates before opening a session")

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=fail_if_session_created,
        )

    assert session_opened is False
    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert (
        executed.error_message
        == "Unsupported command template placeholder(s) in single_arp_query: vpn"
    )


async def test_execute_release_job_marks_invalid_parser_regex_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, parser_rules={"arp_entry_regex": "["})

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "Invalid regex pattern: ["
    assert executed.raw_before_output == "10.44.132.254 0011-2233-4455 S"


async def test_execute_pre_release_query_marks_invalid_parser_regex_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(
            session,
            kind=ReleaseJobKind.PRE_RELEASE_QUERY,
            parser_rules={"arp_entry_regex": "["},
        )

        prepared = await execute_pre_release_query_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert prepared.status == ReleaseJobStatus.FAILED
    assert prepared.phase == ReleaseJobPhase.FINISHED
    assert prepared.error_message == "Invalid regex pattern: ["
    assert prepared.raw_before_output == "10.44.132.254 0011-2233-4455 S"


async def test_execute_release_job_marks_invalid_result_regex_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, error_patterns=["["])

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "Invalid regex pattern: ["


async def test_execute_release_job_marks_invalid_success_regex_failed(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session, success_patterns=["["])

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == "Invalid regex pattern: ["


async def test_execute_release_job_ignores_non_target_arp_rows(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        job = await _seed_job(session)

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(
                before_output="10.44.132.1 aaaa-bbbb-cccc S",
                after_output="10.44.132.1 aaaa-bbbb-cccc D",
            ),
        )

    assert executed.status == ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION
    assert executed.before_state["entry_type"] == "unknown"
    assert executed.after_state == {}


@pytest.mark.parametrize(
    ("credential_active", "profile_active", "switch_enabled", "expected_error"),
    [
        (False, True, True, "Switch credential is inactive"),
        (True, False, True, "Switch command profile is inactive"),
        (True, True, False, "Switch is disabled"),
    ],
)
async def test_execute_release_job_marks_inactive_dependencies_failed(
    session_factory: async_sessionmaker[AsyncSession],
    credential_active: bool,
    profile_active: bool,
    switch_enabled: bool,
    expected_error: str,
) -> None:
    async with session_factory() as session:
        job = await _seed_job(
            session,
            credential_active=credential_active,
            profile_active=profile_active,
            switch_enabled=switch_enabled,
        )

        executed = await execute_release_job(
            session,
            job.id,
            session_factory=_session_factory(),
        )

    assert executed.status == ReleaseJobStatus.FAILED
    assert executed.phase == ReleaseJobPhase.FINISHED
    assert executed.error_message == expected_error
