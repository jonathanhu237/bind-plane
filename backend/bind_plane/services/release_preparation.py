from uuid import UUID

from sqlalchemy.ext.asyncio import AsyncSession

from bind_plane.db.models import RoleName
from bind_plane.domain.ip import parse_single_ipv4
from bind_plane.domain.release import (
    ReleasePreparation,
    ReleasePreparationStatus,
    ReleaseReason,
)
from bind_plane.services.switch_resolution import (
    AmbiguousSwitchMatchError,
    NoSwitchMatchError,
    SelectedSwitchUnavailableError,
    resolve_selected_switch,
    resolve_switch_for_ip,
)


class ReleasePreparationError(ValueError):
    pass


def validate_reason(reason: ReleaseReason | None) -> ReleaseReason:
    if reason is None:
        raise ReleasePreparationError("A release reason is required")
    return reason


async def prepare_release(
    *,
    session: AsyncSession,
    target_ip: str,
    reason: ReleaseReason | None,
    actor_roles: set[RoleName],
    force: bool = False,
    selected_switch_id: UUID | None = None,
) -> ReleasePreparation:
    parsed_ip = parse_single_ipv4(target_ip)
    validated_reason = validate_reason(reason)
    if force and RoleName.ADMIN not in actor_roles:
        raise ReleasePreparationError("Only admin users can force release")
    if selected_switch_id is not None and RoleName.ADMIN not in actor_roles:
        raise ReleasePreparationError("Only admin users can select a switch for forced release")
    if selected_switch_id is not None and not force:
        raise ReleasePreparationError("Explicit switch selection requires forced release")

    try:
        resolved_switch = await resolve_switch_for_ip(session, parsed_ip)
    except NoSwitchMatchError:
        if selected_switch_id is not None:
            try:
                resolved_switch = await resolve_selected_switch(session, selected_switch_id)
            except SelectedSwitchUnavailableError as exc:
                raise ReleasePreparationError(str(exc)) from exc
            return ReleasePreparation(
                status=ReleasePreparationStatus.QUERY_QUEUED,
                target_ip=parsed_ip,
                resolved_switch=resolved_switch,
                observation=None,
                force=force,
                reason=validated_reason,
            )
        return ReleasePreparation(
            status=ReleasePreparationStatus.STOPPED_NO_SWITCH,
            target_ip=parsed_ip,
            resolved_switch=None,
            observation=None,
            force=force,
            reason=validated_reason,
        )
    except AmbiguousSwitchMatchError:
        return ReleasePreparation(
            status=ReleasePreparationStatus.STOPPED_AMBIGUOUS_SWITCH,
            target_ip=parsed_ip,
            resolved_switch=None,
            observation=None,
            force=force,
            reason=validated_reason,
        )

    if selected_switch_id is not None:
        raise ReleasePreparationError(
            "Explicit switch selection is only allowed when automatic resolution has no match"
        )

    return ReleasePreparation(
        status=ReleasePreparationStatus.QUERY_QUEUED,
        target_ip=parsed_ip,
        resolved_switch=resolved_switch,
        observation=None,
        force=force,
        reason=validated_reason,
    )
