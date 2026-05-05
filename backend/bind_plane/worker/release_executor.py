from collections.abc import Callable
from datetime import datetime
from ipaddress import ip_address
from uuid import UUID

from rq.timeouts import JobTimeoutException
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from bind_plane.db.models import (
    ArpEntryType,
    AuditLog,
    CommandProfile,
    Credential,
    ReleaseJob,
    ReleaseJobEvent,
    ReleaseJobKind,
    ReleaseJobPhase,
    ReleaseJobStatus,
    Switch,
    utc_now,
)
from bind_plane.domain.release import ReleasePreparationStatus
from bind_plane.security.credentials import decrypt_secret
from bind_plane.services.command_profiles import (
    CommandProfileError,
    classify_release_result,
    observation_to_dict,
    parse_arp_observation,
    render_commands,
)
from bind_plane.worker.sessions import (
    NetmikoConnectionSettings,
    NetmikoSwitchSession,
    SwitchConnectionTimeoutError,
    SwitchSession,
    SwitchSessionError,
)

SwitchSessionFactory = Callable[[NetmikoConnectionSettings], SwitchSession]

LOGIN_PROMPT_PATTERN_KEYS = {"username_pattern", "password_pattern", "passphrase_pattern"}
PROMPT_PATTERN_KEYS = {
    "connection_options",
    "query_expect_string",
    "query_before_expect_string",
    "query_after_expect_string",
    "release_expect_string",
}


class ReleaseJobExecutorError(RuntimeError):
    pass


class ReleaseJobConfigurationError(RuntimeError):
    pass


def ensure_active_execution_dependencies(job: ReleaseJob) -> None:
    if not job.switch.is_enabled:
        raise ReleaseJobConfigurationError("Switch is disabled")
    if not job.switch.credential.is_active:
        raise ReleaseJobConfigurationError("Switch credential is inactive")
    if not job.command_profile.is_active:
        raise ReleaseJobConfigurationError("Switch command profile is inactive")


def _require_mapping(value: object, name: str) -> dict:
    if value is None:
        return {}
    if not isinstance(value, dict):
        raise ReleaseJobConfigurationError(f"{name} must be an object")
    return value


def _reject_unsupported_keys(value: dict, supported_keys: set[str], name: str) -> None:
    unsupported = sorted(set(value) - supported_keys)
    if unsupported:
        raise ReleaseJobConfigurationError(
            f"Unsupported {name}: {', '.join(unsupported)}"
        )


def _optional_string(value: dict, key: str) -> str | None:
    raw_value = value.get(key)
    if raw_value is None:
        return None
    if not isinstance(raw_value, str) or not raw_value.strip():
        raise ReleaseJobConfigurationError(f"{key} must be a non-empty string")
    return raw_value


def _int_option(value: dict, key: str, default: int) -> int:
    raw_value = value.get(key, default)
    try:
        return int(raw_value)
    except (TypeError, ValueError) as exc:
        raise ReleaseJobConfigurationError(f"Invalid connection option: {key}") from exc


def _float_option(value: dict, key: str, default: float) -> float:
    raw_value = value.get(key, default)
    try:
        return float(raw_value)
    except (TypeError, ValueError) as exc:
        raise ReleaseJobConfigurationError(f"Invalid connection option: {key}") from exc


def _decrypt_credential_secret(value: str, name: str) -> str:
    try:
        return decrypt_secret(value)
    except Exception as exc:
        raise ReleaseJobConfigurationError(f"{name} cannot be decrypted") from exc


async def add_event(
    session: AsyncSession,
    job: ReleaseJob,
    *,
    phase: ReleaseJobPhase,
    status: ReleaseJobStatus,
    message: str | None = None,
    payload: dict | None = None,
) -> None:
    session.add(
        ReleaseJobEvent(
            job_id=job.id,
            phase=phase,
            status=status,
            message=message,
            payload=payload or {},
        )
    )


async def update_job_phase(
    session: AsyncSession,
    job: ReleaseJob,
    *,
    phase: ReleaseJobPhase,
    status: ReleaseJobStatus = ReleaseJobStatus.RUNNING,
    message: str | None = None,
    payload: dict | None = None,
) -> None:
    job.phase = phase
    job.status = status
    await add_event(session, job, phase=phase, status=status, message=message, payload=payload)
    await session.commit()


async def commit_job_checkpoint(session: AsyncSession, job: ReleaseJob) -> None:
    await session.commit()


def capture_partial_release_output(job: ReleaseJob, exc: BaseException) -> None:
    partial_output = getattr(exc, "partial_output", None)
    if isinstance(partial_output, str) and partial_output and not job.raw_release_output:
        job.raw_release_output = partial_output


def _payload_timestamp(value: datetime | None) -> str | None:
    return value.isoformat() if value else None


def _job_timeout_message(exc: BaseException, fallback: str) -> str:
    return str(exc) or fallback


def mark_job_timeout(job: ReleaseJob, exc: BaseException, *, fallback: str) -> None:
    job.status = ReleaseJobStatus.TIMEOUT
    job.phase = ReleaseJobPhase.FINISHED
    job.error_message = _job_timeout_message(exc, fallback)
    job.finished_at = utc_now()


async def load_release_job(
    session: AsyncSession,
    job_id: UUID,
    *,
    for_update: bool = False,
) -> ReleaseJob:
    query = (
        select(ReleaseJob)
        .options(
            selectinload(ReleaseJob.switch).selectinload(Switch.credential),
            selectinload(ReleaseJob.command_profile),
        )
        .where(ReleaseJob.id == job_id)
    )
    if for_update:
        query = query.with_for_update()
    result = await session.execute(query)
    job = result.scalar_one_or_none()
    if job is None:
        raise ReleaseJobExecutorError(f"Release job not found: {job_id}")
    return job


async def claim_job_for_execution(
    session: AsyncSession,
    job_id: UUID,
    *,
    expected_kind: ReleaseJobKind,
) -> tuple[ReleaseJob, bool]:
    job = await load_release_job(session, job_id, for_update=True)
    if job.kind != expected_kind:
        return job, True
    if job.status != ReleaseJobStatus.QUEUED:
        return job, False

    job.started_at = job.started_at or utc_now()
    await update_job_phase(
        session,
        job,
        phase=ReleaseJobPhase.CONNECTING,
        status=ReleaseJobStatus.RUNNING,
        message="Job claimed for execution",
    )
    return job, True


def close_switch_session_safely(
    switch_session: SwitchSession | None,
    job: ReleaseJob,
) -> None:
    if switch_session is None:
        return
    try:
        switch_session.close()
    except JobTimeoutException:
        raise
    except Exception as exc:
        job.result = {
            **job.result,
            "close_error": f"Switch session close failed: {exc.__class__.__name__}",
        }


def build_connection_settings_for_switch(
    *,
    switch: Switch,
    credential: Credential,
    profile: CommandProfile,
) -> NetmikoConnectionSettings:
    login_prompt_patterns = _require_mapping(
        profile.login_prompt_patterns,
        "login_prompt_patterns",
    )
    _reject_unsupported_keys(
        login_prompt_patterns,
        LOGIN_PROMPT_PATTERN_KEYS,
        "login prompt pattern(s)",
    )
    prompt_patterns = _require_mapping(profile.prompt_patterns, "prompt_patterns")
    _reject_unsupported_keys(prompt_patterns, PROMPT_PATTERN_KEYS, "prompt pattern(s)")
    connection_options = _require_mapping(
        prompt_patterns.get("connection_options", {}),
        "prompt_patterns.connection_options",
    )
    query_before_expect_string = _optional_string(
        prompt_patterns,
        "query_before_expect_string",
    ) or _optional_string(prompt_patterns, "query_expect_string")
    query_after_expect_string = _optional_string(
        prompt_patterns,
        "query_after_expect_string",
    ) or _optional_string(prompt_patterns, "query_expect_string")
    passphrase_pattern = _optional_string(login_prompt_patterns, "passphrase_pattern")
    if passphrase_pattern is not None:
        raise ReleaseJobConfigurationError(
            "passphrase_pattern is not supported for Netmiko Telnet sessions"
        )

    return NetmikoConnectionSettings(
        host=switch.management_ip,
        port=_int_option(connection_options, "port", 23),
        device_type=str(connection_options.get("device_type", "hp_comware_telnet")),
        username=credential.username,
        password=_decrypt_credential_secret(credential.encrypted_password, "Switch credential"),
        secret=(
            _decrypt_credential_secret(credential.encrypted_secret, "Switch secret")
            if credential.encrypted_secret
            else None
        ),
        timeout=_int_option(connection_options, "timeout", 30),
        conn_timeout=_int_option(connection_options, "conn_timeout", 20),
        auth_timeout=_int_option(connection_options, "auth_timeout", 20),
        banner_timeout=_int_option(connection_options, "banner_timeout", 20),
        global_delay_factor=_float_option(connection_options, "global_delay_factor", 1.0),
        username_pattern=_optional_string(login_prompt_patterns, "username_pattern"),
        password_pattern=_optional_string(login_prompt_patterns, "password_pattern"),
        passphrase_pattern=passphrase_pattern,
        query_before_expect_string=query_before_expect_string,
        query_after_expect_string=query_after_expect_string,
        release_expect_string=_optional_string(prompt_patterns, "release_expect_string"),
    )


def build_connection_settings(job: ReleaseJob, credential: Credential) -> NetmikoConnectionSettings:
    return build_connection_settings_for_switch(
        switch=job.switch,
        credential=credential,
        profile=job.command_profile,
    )


async def write_audit_log(session: AsyncSession, job: ReleaseJob) -> None:
    session.add(
        AuditLog(
            actor_id=job.operator_id,
            action="release_job_finished",
            target_type="release_job",
            target_id=job.id,
            payload={
                "target_ip": job.target_ip,
                "switch_id": str(job.switch_id),
                "command_profile_id": str(job.command_profile_id),
                "status": job.status,
                "phase": job.phase,
                "error_message": job.error_message,
                "force": job.force,
                "reason": job.reason,
                "started_at": _payload_timestamp(job.started_at),
                "finished_at": _payload_timestamp(job.finished_at),
                "before_state": job.before_state,
                "after_state": job.after_state,
                "result": job.result,
                "raw_before_output": job.raw_before_output,
                "raw_release_output": job.raw_release_output,
                "raw_after_output": job.raw_after_output,
            },
        )
    )


async def write_pre_release_audit_log(session: AsyncSession, job: ReleaseJob) -> None:
    session.add(
        AuditLog(
            actor_id=job.operator_id,
            action="release_pre_query_completed",
            target_type="release_job",
            target_id=job.id,
            payload={
                "target_ip": job.target_ip,
                "switch_id": str(job.switch_id),
                "command_profile_id": str(job.command_profile_id),
                "status": job.status,
                "phase": job.phase,
                "force": job.force,
                "reason": job.reason,
                "started_at": _payload_timestamp(job.started_at),
                "finished_at": _payload_timestamp(job.finished_at),
                "before_state": job.before_state,
                "result": job.result,
                "error_message": job.error_message,
                "raw_before_output": job.raw_before_output,
            },
        )
    )


async def execute_pre_release_query_job(
    session: AsyncSession,
    job_id: UUID,
    *,
    session_factory: SwitchSessionFactory = NetmikoSwitchSession,
) -> ReleaseJob:
    job, should_finalize = await claim_job_for_execution(
        session,
        job_id,
        expected_kind=ReleaseJobKind.PRE_RELEASE_QUERY,
    )
    if not should_finalize:
        return job
    switch_session: SwitchSession | None = None

    try:
        try:
            target_ip = ip_address(job.target_ip)
        except ValueError as exc:
            raise ReleaseJobConfigurationError("Release jobs only support IPv4 targets") from exc
        if target_ip.version != 4:
            raise ReleaseJobConfigurationError("Release jobs only support IPv4 targets")
        if job.kind != ReleaseJobKind.PRE_RELEASE_QUERY:
            raise ReleaseJobConfigurationError("Job is not a pre-release query")
        ensure_active_execution_dependencies(job)
        credential = job.switch.credential
        settings = build_connection_settings(job, credential)
        commands = render_commands(
            job.command_profile.command_templates,
            target_ip,
            pagination_rules=job.command_profile.pagination_rules,
        )

        switch_session = session_factory(settings)
        switch_session.connect()

        await update_job_phase(session, job, phase=ReleaseJobPhase.QUERYING_BEFORE)
        before_output = switch_session.query_before(commands)
        job.raw_before_output = before_output
        before = parse_arp_observation(
            target_ip=target_ip,
            output=before_output,
            parser_rules=job.command_profile.parser_rules,
        )
        job.before_state = observation_to_dict(before)
        await commit_job_checkpoint(session, job)

        if before.entry_type is ArpEntryType.UNKNOWN:
            job.status = ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION
            job.phase = ReleaseJobPhase.FINISHED
            job.finished_at = utc_now()
            job.result = {
                "preparation_status": ReleasePreparationStatus.NEEDS_MANUAL_CONFIRMATION,
                "message": "Pre-release state could not be parsed confidently",
                "before_state": job.before_state,
            }
        elif before.entry_type is ArpEntryType.MISSING and not job.force:
            job.status = ReleaseJobStatus.CANCELLED
            job.phase = ReleaseJobPhase.FINISHED
            job.finished_at = utc_now()
            job.result = {
                "preparation_status": ReleasePreparationStatus.STOPPED_NO_RECORD,
                "message": "No pre-release record was found; normal release stopped",
                "before_state": job.before_state,
            }
        else:
            if before.entry_type is not ArpEntryType.MISSING:
                job.force = False
            job.status = ReleaseJobStatus.WAITING_CONFIRMATION
            job.phase = ReleaseJobPhase.WAITING_CONFIRMATION
            job.result = {
                "preparation_status": ReleasePreparationStatus.READY,
                "message": "Pre-release query complete; waiting for confirmation",
                "before_state": job.before_state,
            }
    except SwitchConnectionTimeoutError as exc:
        mark_job_timeout(job, exc, fallback="Pre-release query timed out")
    except JobTimeoutException as exc:
        mark_job_timeout(job, exc, fallback="RQ pre-release query timed out")
    except (SwitchSessionError, CommandProfileError, ReleaseJobConfigurationError) as exc:
        job.status = ReleaseJobStatus.FAILED
        job.phase = ReleaseJobPhase.FINISHED
        job.error_message = str(exc)
        job.finished_at = utc_now()
    except Exception as exc:
        job.status = ReleaseJobStatus.FAILED
        job.phase = ReleaseJobPhase.FINISHED
        job.error_message = f"Unexpected pre-release query error: {exc.__class__.__name__}"
        job.finished_at = utc_now()
    finally:
        try:
            close_switch_session_safely(switch_session, job)
        except JobTimeoutException as exc:
            mark_job_timeout(job, exc, fallback="RQ pre-release query timed out while closing")
        await add_event(
            session,
            job,
            phase=job.phase,
            status=job.status,
            message=job.error_message or job.result.get("message"),
            payload=job.result,
        )
        await write_pre_release_audit_log(session, job)
        await session.commit()

    return job


async def execute_release_job(
    session: AsyncSession,
    job_id: UUID,
    *,
    session_factory: SwitchSessionFactory = NetmikoSwitchSession,
) -> ReleaseJob:
    job, should_finalize = await claim_job_for_execution(
        session,
        job_id,
        expected_kind=ReleaseJobKind.RELEASE,
    )
    if not should_finalize:
        return job
    switch_session: SwitchSession | None = None

    try:
        try:
            target_ip = ip_address(job.target_ip)
        except ValueError as exc:
            raise ReleaseJobConfigurationError("Release jobs only support IPv4 targets") from exc
        if target_ip.version != 4:
            raise ReleaseJobConfigurationError("Release jobs only support IPv4 targets")
        if job.kind != ReleaseJobKind.RELEASE:
            raise ReleaseJobConfigurationError("Job is not a confirmed release")
        ensure_active_execution_dependencies(job)
        credential = job.switch.credential
        settings = build_connection_settings(job, credential)
        commands = render_commands(
            job.command_profile.command_templates,
            target_ip,
            pagination_rules=job.command_profile.pagination_rules,
        )

        switch_session = session_factory(settings)
        switch_session.connect()

        await update_job_phase(session, job, phase=ReleaseJobPhase.QUERYING_BEFORE)
        before_output = switch_session.query_before(commands)
        job.raw_before_output = before_output
        before = parse_arp_observation(
            target_ip=target_ip,
            output=before_output,
            parser_rules=job.command_profile.parser_rules,
        )
        job.before_state = observation_to_dict(before)
        await commit_job_checkpoint(session, job)

        if before.entry_type is ArpEntryType.UNKNOWN:
            job.status = ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION
            job.phase = ReleaseJobPhase.FINISHED
            job.result = {
                "message": "Pre-release state could not be parsed confidently",
                "before_state": job.before_state,
            }
            return job
        if before.entry_type is ArpEntryType.MISSING and not job.force:
            job.status = ReleaseJobStatus.CANCELLED
            job.phase = ReleaseJobPhase.FINISHED
            job.result = {
                "message": "No pre-release record was found; release stopped before command",
                "before_state": job.before_state,
            }
            return job

        await update_job_phase(session, job, phase=ReleaseJobPhase.RELEASING)
        release_output = switch_session.release(commands)
        job.raw_release_output = release_output
        await commit_job_checkpoint(session, job)

        await update_job_phase(session, job, phase=ReleaseJobPhase.QUERYING_AFTER)
        after_output = switch_session.query_after(commands)
        job.raw_after_output = after_output
        after = parse_arp_observation(
            target_ip=target_ip,
            output=after_output,
            parser_rules=job.command_profile.parser_rules,
        )
        job.after_state = observation_to_dict(after)
        await commit_job_checkpoint(session, job)

        await update_job_phase(session, job, phase=ReleaseJobPhase.CLASSIFYING)
        classified = classify_release_result(
            release_output=release_output,
            after_observation=after,
            error_patterns=job.command_profile.error_patterns,
            success_patterns=job.command_profile.success_patterns,
        )
        job.status = classified.status
        job.phase = ReleaseJobPhase.FINISHED
        job.result = {"message": classified.message, **classified.details}
    except SwitchConnectionTimeoutError as exc:
        capture_partial_release_output(job, exc)
        mark_job_timeout(job, exc, fallback="Release job timed out")
    except JobTimeoutException as exc:
        mark_job_timeout(job, exc, fallback="RQ release job timed out")
    except (SwitchSessionError, CommandProfileError, ReleaseJobConfigurationError) as exc:
        capture_partial_release_output(job, exc)
        job.status = ReleaseJobStatus.FAILED
        job.phase = ReleaseJobPhase.FINISHED
        job.error_message = str(exc)
    except Exception as exc:
        job.status = ReleaseJobStatus.FAILED
        job.phase = ReleaseJobPhase.FINISHED
        job.error_message = f"Unexpected release job error: {exc.__class__.__name__}"
    finally:
        try:
            close_switch_session_safely(switch_session, job)
        except JobTimeoutException as exc:
            mark_job_timeout(job, exc, fallback="RQ release job timed out while closing")
        job.finished_at = utc_now()
        await add_event(
            session,
            job,
            phase=job.phase,
            status=job.status,
            message=job.error_message,
            payload=job.result,
        )
        await write_audit_log(session, job)
        await session.commit()

    return job
