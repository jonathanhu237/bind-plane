from collections.abc import Callable
from typing import Annotated, Literal
from uuid import UUID

from fastapi import APIRouter, HTTPException, Query, status
from sqlalchemy import or_, select
from sqlalchemy.exc import IntegrityError
from sqlalchemy.orm import selectinload

from bind_plane.api.deps import (
    OperatorUserDep,
    PreReleaseEnqueuerDep,
    ReleaseEnqueuerDep,
    SessionDep,
)
from bind_plane.api.pagination import apply_sort, paginate_query
from bind_plane.db.models import (
    ArpEntryType,
    AuditLog,
    ReleaseJob,
    ReleaseJobEvent,
    ReleaseJobKind,
    ReleaseJobPhase,
    ReleaseJobStatus,
    RoleName,
    Switch,
    User,
    utc_now,
)
from bind_plane.domain.ip import IPv4TargetError
from bind_plane.domain.release import (
    ArpObservation,
    ReleasePreparation,
    ReleasePreparationStatus,
    ReleaseReason,
    ResolvedSwitch,
)
from bind_plane.schemas.pagination import PaginatedResponse
from bind_plane.schemas.release import (
    ArpObservationRead,
    OperatorSummaryRead,
    RawReleaseOutputRead,
    ReleaseJobCreated,
    ReleaseJobCreateRequest,
    ReleaseJobEventRead,
    ReleaseJobRead,
    ReleasePreparationRead,
    ReleasePrepareRequest,
    ResolvedSwitchRead,
    SwitchSummaryRead,
)
from bind_plane.services.command_profiles import CommandProfileError
from bind_plane.services.release_preparation import ReleasePreparationError, prepare_release
from bind_plane.worker.sessions import SwitchConnectionTimeoutError, SwitchSessionError

router = APIRouter(prefix="/releases", tags=["releases"])
QUEUE_ENQUEUED_MESSAGE = "Queue enqueue confirmed"


def _role_names(user: User) -> set[RoleName]:
    return {RoleName(role.role) for role in user.roles}


def _is_admin(user: User) -> bool:
    return RoleName.ADMIN in _role_names(user)


def _observation_read(observation: ArpObservation | None) -> ArpObservationRead | None:
    if observation is None:
        return None
    return ArpObservationRead(
        entry_type=observation.entry_type,
        mac=observation.mac,
        raw_output=observation.raw_output,
    )


def _switch_read(resolved_switch: ResolvedSwitch | None) -> ResolvedSwitchRead | None:
    if resolved_switch is None:
        return None
    selection_source = (
        "selected_switch" if resolved_switch.network_id is None else "resolved_network"
    )
    return ResolvedSwitchRead(
        switch_id=resolved_switch.switch_id,
        network_id=resolved_switch.network_id,
        command_profile_id=resolved_switch.command_profile_id,
        management_ip=resolved_switch.management_ip,
        name=resolved_switch.name,
        cidr=resolved_switch.cidr,
        prefix_length=resolved_switch.prefix_length,
        selection_source=selection_source,
    )


def _preparation_read(
    preparation: ReleasePreparation,
    *,
    preparation_job_id: UUID | None = None,
) -> ReleasePreparationRead:
    return ReleasePreparationRead(
        preparation_job_id=preparation_job_id,
        status=preparation.status,
        target_ip=str(preparation.target_ip),
        resolved_switch=_switch_read(preparation.resolved_switch),
        observation=_observation_read(preparation.observation),
        force=preparation.force,
        reason=preparation.reason,
    )


def _job_read(job: ReleaseJob, *, include_raw: bool) -> ReleaseJobRead:
    events = sorted(job.events, key=lambda event: event.created_at)
    return ReleaseJobRead(
        id=job.id,
        target_ip=job.target_ip,
        kind=job.kind,
        reason=ReleaseReason(job.reason),
        ticket_id=job.ticket_id,
        force=job.force,
        status=job.status,
        phase=job.phase,
        before_state=job.before_state,
        after_state=job.after_state,
        result=job.result,
        error_message=job.error_message,
        operator=OperatorSummaryRead(
            id=job.operator.id,
            username=job.operator.username,
            display_name=job.operator.display_name,
        ),
        switch=SwitchSummaryRead(
            id=job.switch.id,
            name=job.switch.name,
            management_ip=job.switch.management_ip,
        ),
        retry_of_id=job.retry_of_id,
        preparation_job_id=job.preparation_job_id,
        raw_output=RawReleaseOutputRead(
            before=job.raw_before_output,
            release=job.raw_release_output,
            after=job.raw_after_output,
        )
        if include_raw
        else None,
        events=[ReleaseJobEventRead.model_validate(event) for event in events],
        created_at=job.created_at,
        updated_at=job.updated_at,
        started_at=job.started_at,
        finished_at=job.finished_at,
    )


def _release_queue_function_name(job: ReleaseJob) -> str:
    if job.kind == ReleaseJobKind.PRE_RELEASE_QUERY:
        return "bind_plane.worker.main.run_pre_release_query_job"
    return "bind_plane.worker.main.run_release_job"


def _has_enqueue_confirmation(job: ReleaseJob) -> bool:
    return any(event.message == QUEUE_ENQUEUED_MESSAGE for event in job.events)


def _raise_preparation_error(exc: Exception) -> None:
    if isinstance(exc, IPv4TargetError):
        raise HTTPException(status_code=status.HTTP_422_UNPROCESSABLE_ENTITY, detail=str(exc))
    if isinstance(exc, ReleasePreparationError):
        status_code = (
            status.HTTP_403_FORBIDDEN
            if "Only admin" in str(exc)
            else status.HTTP_409_CONFLICT
        )
        raise HTTPException(status_code=status_code, detail=str(exc))
    if isinstance(exc, CommandProfileError):
        raise HTTPException(status_code=status.HTTP_409_CONFLICT, detail=str(exc))
    if isinstance(exc, SwitchConnectionTimeoutError):
        raise HTTPException(status_code=status.HTTP_504_GATEWAY_TIMEOUT, detail=str(exc))
    if isinstance(exc, SwitchSessionError):
        raise HTTPException(status_code=status.HTTP_502_BAD_GATEWAY, detail=str(exc))
    raise exc


async def _prepare(
    *,
    payload: ReleasePrepareRequest | ReleaseJobCreateRequest,
    session: SessionDep,
    current_user: User,
) -> ReleasePreparation:
    try:
        return await prepare_release(
            session=session,
            target_ip=payload.target_ip,
            reason=payload.reason,
            actor_roles=_role_names(current_user),
            force=payload.force,
            selected_switch_id=getattr(payload, "selected_switch_id", None),
        )
    except Exception as exc:
        _raise_preparation_error(exc)
        raise


def _enqueue_failure_detail(job: ReleaseJob) -> dict[str, str]:
    return {"message": "Queue enqueue failed", "job_id": str(job.id)}


async def _mark_enqueue_failed(
    *,
    session: SessionDep,
    job: ReleaseJob,
    action: str,
    error_type: str,
) -> None:
    job.status = ReleaseJobStatus.FAILED
    job.phase = ReleaseJobPhase.FINISHED
    job.error_message = "Queue enqueue failed"
    job.finished_at = utc_now()
    session.add(
        ReleaseJobEvent(
            job_id=job.id,
            phase=ReleaseJobPhase.FINISHED,
            status=ReleaseJobStatus.FAILED,
            message=job.error_message,
            payload={"error_type": error_type},
        )
    )
    session.add(
        AuditLog(
            actor_id=job.operator_id,
            action=action,
            target_type="release_job",
            target_id=job.id,
            payload={
                "target_ip": job.target_ip,
                "switch_id": str(job.switch_id),
                "command_profile_id": str(job.command_profile_id),
                "force": job.force,
                "reason": job.reason,
                "error_type": error_type,
            },
        )
    )
    await session.commit()


async def _enqueue_or_mark_failed(
    *,
    session: SessionDep,
    job: ReleaseJob,
    enqueue: Callable[[UUID], None],
    failure_action: str,
) -> None:
    try:
        enqueue(job.id)
    except Exception as exc:
        await _mark_enqueue_failed(
            session=session,
            job=job,
            action=failure_action,
            error_type=exc.__class__.__name__,
        )
        raise HTTPException(
            status_code=status.HTTP_503_SERVICE_UNAVAILABLE,
            detail=_enqueue_failure_detail(job),
        ) from exc
    session.add(
        ReleaseJobEvent(
            job_id=job.id,
            phase=job.phase,
            status=job.status,
            message=QUEUE_ENQUEUED_MESSAGE,
            payload={"function": _release_queue_function_name(job)},
        )
    )
    await session.commit()


async def _get_visible_job(
    *,
    session: SessionDep,
    current_user: User,
    job_id: UUID,
    for_update: bool = False,
) -> ReleaseJob:
    query = (
        select(ReleaseJob)
        .options(
            selectinload(ReleaseJob.operator).selectinload(User.roles),
            selectinload(ReleaseJob.switch),
            selectinload(ReleaseJob.events),
        )
        .where(ReleaseJob.id == job_id)
    )
    if for_update:
        query = query.with_for_update()
    result = await session.execute(query)
    job = result.scalar_one_or_none()
    if job is None or (not _is_admin(current_user) and job.operator_id != current_user.id):
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="Job not found")
    return job


async def _get_release_for_preparation(
    *,
    session: SessionDep,
    current_user: User,
    preparation_job_id: UUID,
    current_user_id: UUID | None = None,
    current_user_is_admin: bool | None = None,
) -> ReleaseJob | None:
    is_admin = _is_admin(current_user) if current_user_is_admin is None else current_user_is_admin
    operator_id = current_user.id if current_user_id is None else current_user_id
    query = (
        select(ReleaseJob)
        .options(
            selectinload(ReleaseJob.operator).selectinload(User.roles),
            selectinload(ReleaseJob.switch),
            selectinload(ReleaseJob.events),
        )
        .where(
            ReleaseJob.kind == ReleaseJobKind.RELEASE,
            ReleaseJob.preparation_job_id == preparation_job_id,
        )
    )
    if not is_admin:
        query = query.where(ReleaseJob.operator_id == operator_id)
    result = await session.execute(query)
    return result.scalar_one_or_none()


async def _existing_release_response(
    *,
    session: SessionDep,
    current_user: User,
    preparation_job: ReleaseJob,
    enqueue_release: Callable[[UUID], None] | None = None,
) -> ReleaseJobCreated | None:
    release_job_id = preparation_job.result.get("release_job_id")
    if not isinstance(release_job_id, str):
        return None
    release = await _get_release_for_preparation(
        session=session,
        current_user=current_user,
        preparation_job_id=preparation_job.id,
    )
    if release is None or str(release.id) != release_job_id:
        return None
    if (
        enqueue_release is not None
        and release.status == ReleaseJobStatus.QUEUED
        and not _has_enqueue_confirmation(release)
    ):
        await _enqueue_or_mark_failed(
            session=session,
            job=release,
            enqueue=enqueue_release,
            failure_action="release_job_enqueue_failed",
        )
    return ReleaseJobCreated(job_id=release.id, status=release.status, phase=release.phase)


def _is_stopped_no_record_preparation(job: ReleaseJob) -> bool:
    return (
        job.kind == ReleaseJobKind.PRE_RELEASE_QUERY
        and job.status == ReleaseJobStatus.CANCELLED
        and job.result.get("preparation_status")
        == ReleasePreparationStatus.STOPPED_NO_RECORD
        and job.before_state.get("entry_type") == ArpEntryType.MISSING
    )


def _switch_selection_payload(preparation: ReleasePreparation) -> dict[str, str]:
    if preparation.resolved_switch is None:
        return {}
    if preparation.resolved_switch.network_id is None:
        return {
            "switch_selection": "selected_switch",
            "selected_switch_id": str(preparation.resolved_switch.switch_id),
        }
    return {
        "switch_selection": "resolved_network",
        "network_id": str(preparation.resolved_switch.network_id),
        "cidr": preparation.resolved_switch.cidr,
    }


def _switch_selection_from_job(job: ReleaseJob) -> dict[str, str | None]:
    keys = {"switch_selection", "selected_switch_id", "network_id", "cidr"}
    if isinstance(job.result.get("switch_selection"), str):
        return {key: job.result.get(key) for key in keys if job.result.get(key) is not None}
    for event in job.events:
        if isinstance(event.payload.get("switch_selection"), str):
            return {
                key: event.payload.get(key)
                for key in keys
                if event.payload.get(key) is not None
            }
    return {"switch_selection": "resolved_network"}


async def _get_active_retry(
    *,
    session: SessionDep,
    current_user: User,
    original_id: UUID,
) -> ReleaseJob | None:
    query = (
        select(ReleaseJob)
        .options(
            selectinload(ReleaseJob.operator).selectinload(User.roles),
            selectinload(ReleaseJob.switch),
            selectinload(ReleaseJob.events),
        )
        .where(
            ReleaseJob.kind == ReleaseJobKind.RELEASE,
            ReleaseJob.retry_of_id == original_id,
            ReleaseJob.status.in_(
                [ReleaseJobStatus.QUEUED, ReleaseJobStatus.RUNNING]
            ),
        )
        .order_by(ReleaseJob.created_at)
    )
    if not _is_admin(current_user):
        query = query.where(ReleaseJob.operator_id == current_user.id)
    result = await session.execute(query)
    return result.scalars().first()


@router.get("/reasons")
async def list_reasons(_: OperatorUserDep) -> list[ReleaseReason]:
    return list(ReleaseReason)


@router.post("/prepare")
async def prepare_release_endpoint(
    payload: ReleasePrepareRequest,
    session: SessionDep,
    current_user: OperatorUserDep,
    enqueue_pre_release_query: PreReleaseEnqueuerDep,
) -> ReleasePreparationRead:
    preparation = await _prepare(
        payload=payload,
        session=session,
        current_user=current_user,
    )
    if preparation.status is not ReleasePreparationStatus.QUERY_QUEUED:
        return _preparation_read(preparation)
    if preparation.resolved_switch is None or preparation.reason is None:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Release preparation did not resolve a switch",
        )

    job = ReleaseJob(
        target_ip=str(preparation.target_ip),
        kind=ReleaseJobKind.PRE_RELEASE_QUERY,
        reason=preparation.reason,
        force=preparation.force,
        status=ReleaseJobStatus.QUEUED,
        phase=ReleaseJobPhase.QUEUED,
        operator_id=current_user.id,
        switch_id=preparation.resolved_switch.switch_id,
        command_profile_id=preparation.resolved_switch.command_profile_id,
        result=_switch_selection_payload(preparation),
    )
    session.add(job)
    await session.flush()
    session.add(
        ReleaseJobEvent(
            job_id=job.id,
            phase=ReleaseJobPhase.QUEUED,
            status=ReleaseJobStatus.QUEUED,
            message="Pre-release query queued",
            payload={"force": job.force, **_switch_selection_payload(preparation)},
        )
    )
    session.add(
        AuditLog(
            actor_id=current_user.id,
            action="release_pre_query_queued",
            target_type="release_job",
            target_id=job.id,
            payload={
                "target_ip": job.target_ip,
                "switch_id": str(job.switch_id),
                "command_profile_id": str(job.command_profile_id),
                "force": job.force,
                "reason": job.reason,
                **_switch_selection_payload(preparation),
            },
        )
    )
    await session.commit()
    await _enqueue_or_mark_failed(
        session=session,
        job=job,
        enqueue=enqueue_pre_release_query,
        failure_action="release_pre_query_enqueue_failed",
    )
    return _preparation_read(preparation, preparation_job_id=job.id)


@router.post("/jobs")
async def create_release_job(
    payload: ReleaseJobCreateRequest,
    session: SessionDep,
    current_user: OperatorUserDep,
    enqueue_release: ReleaseEnqueuerDep,
) -> ReleaseJobCreated:
    if not payload.confirmed:
        raise HTTPException(
            status_code=status.HTTP_400_BAD_REQUEST,
            detail="Explicit confirmation is required",
        )

    job = await _get_visible_job(
        session=session,
        current_user=current_user,
        job_id=payload.preparation_job_id,
        for_update=True,
    )
    current_user_is_admin = _is_admin(current_user)
    force_after_no_record = payload.force and _is_stopped_no_record_preparation(job)
    if job.operator_id != current_user.id:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Only the user who prepared the release can confirm it",
        )
    if job.kind != ReleaseJobKind.PRE_RELEASE_QUERY:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Release has already been confirmed",
        )
    if job.target_ip != payload.target_ip or job.reason != payload.reason:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Release confirmation does not match prepared release",
        )
    if job.force != payload.force and not force_after_no_record:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Release confirmation does not match prepared release",
        )
    if (job.force or payload.force or force_after_no_record) and not current_user_is_admin:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Only admin can force release jobs",
        )
    if job.status != ReleaseJobStatus.WAITING_CONFIRMATION:
        if job.status == ReleaseJobStatus.SUCCEEDED:
            existing_response = await _existing_release_response(
                session=session,
                current_user=current_user,
                preparation_job=job,
                enqueue_release=enqueue_release,
            )
            if existing_response is not None:
                return existing_response
        if not force_after_no_record:
            raise HTTPException(
                status_code=status.HTTP_409_CONFLICT,
                detail={"status": job.status, "message": "Release preparation is not ready"},
            )

    preparation_job_id = job.id
    current_user_id = current_user.id
    release_force = True if force_after_no_record else job.force
    selection_context = _switch_selection_from_job(job)
    release = ReleaseJob(
        target_ip=job.target_ip,
        kind=ReleaseJobKind.RELEASE,
        reason=job.reason,
        ticket_id=payload.ticket_id or None,
        force=release_force,
        status=ReleaseJobStatus.QUEUED,
        phase=ReleaseJobPhase.QUEUED,
        before_state=job.before_state,
        result=selection_context,
        raw_before_output=job.raw_before_output,
        operator_id=current_user.id,
        switch_id=job.switch_id,
        command_profile_id=job.command_profile_id,
        preparation_job_id=preparation_job_id,
    )
    session.add(release)
    try:
        await session.flush()
    except IntegrityError:
        await session.rollback()
        existing_release = await _get_release_for_preparation(
            session=session,
            current_user=current_user,
            preparation_job_id=preparation_job_id,
            current_user_id=current_user_id,
            current_user_is_admin=current_user_is_admin,
        )
        if existing_release is not None:
            return ReleaseJobCreated(
                job_id=existing_release.id,
                status=existing_release.status,
                phase=existing_release.phase,
            )
        raise

    job.force = release_force
    job.status = ReleaseJobStatus.SUCCEEDED
    job.phase = ReleaseJobPhase.FINISHED
    job.finished_at = utc_now()
    job.result = {
        **job.result,
        "preparation_status": ReleasePreparationStatus.READY,
        "message": "Pre-release query confirmed; release job queued",
        "release_job_id": str(release.id),
    }
    session.add(
        ReleaseJobEvent(
            job_id=job.id,
            phase=ReleaseJobPhase.FINISHED,
            status=ReleaseJobStatus.SUCCEEDED,
            message="Pre-release query confirmed",
            payload={"release_job_id": str(release.id)},
        )
    )
    session.add(
        ReleaseJobEvent(
            job_id=release.id,
            phase=ReleaseJobPhase.QUEUED,
            status=ReleaseJobStatus.QUEUED,
            message="Release job queued",
            payload={"preparation_job_id": str(job.id)},
        )
    )
    session.add(
        AuditLog(
            actor_id=current_user.id,
            action="release_job_created",
            target_type="release_job",
            target_id=release.id,
            payload={
                "target_ip": release.target_ip,
                "switch_id": str(release.switch_id),
                "command_profile_id": str(release.command_profile_id),
                "force": release.force,
                "reason": release.reason,
                "ticket_id": release.ticket_id,
                "preparation_job_id": str(job.id),
                "before_state": release.before_state,
                "raw_before_output": release.raw_before_output,
                **selection_context,
            },
        )
    )
    session.add(
        AuditLog(
            actor_id=current_user.id,
            action="release_pre_query_confirmed",
            target_type="release_job",
            target_id=job.id,
            payload={
                "target_ip": job.target_ip,
                "switch_id": str(job.switch_id),
                "command_profile_id": str(job.command_profile_id),
                "force": release.force,
                "reason": job.reason,
                "release_job_id": str(release.id),
                **selection_context,
            },
        )
    )
    await session.commit()
    await _enqueue_or_mark_failed(
        session=session,
        job=release,
        enqueue=enqueue_release,
        failure_action="release_job_enqueue_failed",
    )
    return ReleaseJobCreated(job_id=release.id, status=release.status, phase=release.phase)


@router.get("/jobs")
async def list_release_jobs(
    session: SessionDep,
    current_user: OperatorUserDep,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=25, ge=1, le=200),
    search: str | None = Query(default=None, max_length=128),
    status_filter: Annotated[ReleaseJobStatus | None, Query(alias="status")] = None,
    kind: ReleaseJobKind | None = None,
    force: bool | None = None,
    sort_by: str = Query(default="created_at", max_length=64),
    sort_order: Literal["asc", "desc"] = Query(default="desc"),
) -> PaginatedResponse[ReleaseJobRead]:
    query = (
        select(ReleaseJob)
        .options(
            selectinload(ReleaseJob.operator),
            selectinload(ReleaseJob.switch),
            selectinload(ReleaseJob.events),
        )
    )
    if not _is_admin(current_user):
        query = query.where(ReleaseJob.operator_id == current_user.id)
    if search:
        search_term = f"%{search.strip()}%"
        query = query.where(
            or_(
                ReleaseJob.target_ip.ilike(search_term),
                ReleaseJob.reason.ilike(search_term),
                ReleaseJob.ticket_id.ilike(search_term),
                ReleaseJob.switch.has(Switch.name.ilike(search_term)),
                ReleaseJob.operator.has(User.username.ilike(search_term)),
            )
        )
    if status_filter is not None:
        query = query.where(ReleaseJob.status == status_filter)
    if kind is not None:
        query = query.where(ReleaseJob.kind == kind)
    if force is not None:
        query = query.where(ReleaseJob.force == force)
    query = apply_sort(
        query,
        sort_by=sort_by,
        sort_order=sort_order,
        allowed={
            "created_at": ReleaseJob.created_at,
            "updated_at": ReleaseJob.updated_at,
            "target_ip": ReleaseJob.target_ip,
            "status": ReleaseJob.status,
            "phase": ReleaseJob.phase,
        },
    )
    return await paginate_query(
        session,
        query,
        page=page,
        page_size=page_size,
        item_factory=lambda job: _job_read(job, include_raw=False),
    )


@router.get("/jobs/{job_id}")
async def get_release_job(
    job_id: UUID,
    session: SessionDep,
    current_user: OperatorUserDep,
) -> ReleaseJobRead:
    job = await _get_visible_job(session=session, current_user=current_user, job_id=job_id)
    return _job_read(job, include_raw=_is_admin(current_user))


@router.post("/jobs/{job_id}/retry")
async def retry_release_job(
    job_id: UUID,
    session: SessionDep,
    current_user: OperatorUserDep,
    enqueue_release: ReleaseEnqueuerDep,
) -> ReleaseJobCreated:
    original = await _get_visible_job(
        session=session,
        current_user=current_user,
        job_id=job_id,
        for_update=True,
    )
    if original.kind != ReleaseJobKind.RELEASE:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Only confirmed release jobs can be retried",
        )
    if original.status not in {
        ReleaseJobStatus.FAILED,
        ReleaseJobStatus.TIMEOUT,
        ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION,
    }:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Only failed, timed out, or manual-confirmation jobs can be retried",
        )
    if original.force and not _is_admin(current_user):
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Only admin can retry forced release jobs",
        )

    active_retry = await _get_active_retry(
        session=session,
        current_user=current_user,
        original_id=original.id,
    )
    if active_retry is not None:
        if (
            active_retry.status == ReleaseJobStatus.QUEUED
            and not _has_enqueue_confirmation(active_retry)
        ):
            await _enqueue_or_mark_failed(
                session=session,
                job=active_retry,
                enqueue=enqueue_release,
                failure_action="release_retry_enqueue_failed",
            )
        return ReleaseJobCreated(
            job_id=active_retry.id,
            status=active_retry.status,
            phase=active_retry.phase,
        )

    retry = ReleaseJob(
        target_ip=original.target_ip,
        kind=ReleaseJobKind.RELEASE,
        reason=original.reason,
        ticket_id=original.ticket_id,
        force=original.force,
        status=ReleaseJobStatus.QUEUED,
        phase=ReleaseJobPhase.QUEUED,
        before_state=original.before_state,
        operator_id=current_user.id,
        switch_id=original.switch_id,
        command_profile_id=original.command_profile_id,
        retry_of_id=original.id,
    )
    session.add(retry)
    await session.flush()
    session.add(
        ReleaseJobEvent(
            job_id=retry.id,
            phase=ReleaseJobPhase.QUEUED,
            status=ReleaseJobStatus.QUEUED,
            message="Release retry queued",
            payload={"retry_of_id": str(original.id)},
        )
    )
    session.add(
        AuditLog(
            actor_id=current_user.id,
            action="release_job_retried",
            target_type="release_job",
            target_id=retry.id,
            payload={
                "target_ip": retry.target_ip,
                "switch_id": str(retry.switch_id),
                "command_profile_id": str(retry.command_profile_id),
                "force": retry.force,
                "reason": retry.reason,
                "retry_of_id": str(original.id),
            },
        )
    )
    await session.commit()
    await _enqueue_or_mark_failed(
        session=session,
        job=retry,
        enqueue=enqueue_release,
        failure_action="release_retry_enqueue_failed",
    )
    return ReleaseJobCreated(job_id=retry.id, status=retry.status, phase=retry.phase)
