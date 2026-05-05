from uuid import UUID

from fastapi import APIRouter, HTTPException, status
from sqlalchemy import select

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.db.models import CommandProfile
from bind_plane.schemas.admin import (
    CommandProfileCreate,
    CommandProfileRead,
    CommandProfileUpdate,
)

router = APIRouter(prefix="/admin/command-profiles", tags=["admin-command-profiles"])


@router.get("")
async def list_command_profiles(
    session: SessionDep,
    _: AdminUserDep,
) -> list[CommandProfileRead]:
    result = await session.execute(select(CommandProfile).order_by(CommandProfile.name))
    return [CommandProfileRead.model_validate(profile) for profile in result.scalars().all()]


@router.post("")
async def create_command_profile(
    payload: CommandProfileCreate,
    session: SessionDep,
    _: AdminUserDep,
) -> CommandProfileRead:
    existing = await session.scalar(
        select(CommandProfile).where(CommandProfile.name == payload.name)
    )
    if existing is not None:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Command profile name already exists",
        )

    profile = CommandProfile(**payload.model_dump())
    session.add(profile)
    await session.commit()
    await session.refresh(profile)
    return CommandProfileRead.model_validate(profile)


@router.patch("/{profile_id}")
async def update_command_profile(
    profile_id: UUID,
    payload: CommandProfileUpdate,
    session: SessionDep,
    _: AdminUserDep,
) -> CommandProfileRead:
    profile = await session.get(CommandProfile, profile_id)
    if profile is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="Command profile not found",
        )

    for field, value in payload.model_dump(exclude_unset=True).items():
        setattr(profile, field, value)

    await session.commit()
    await session.refresh(profile)
    return CommandProfileRead.model_validate(profile)


@router.delete("/{profile_id}")
async def deactivate_command_profile(
    profile_id: UUID,
    session: SessionDep,
    _: AdminUserDep,
) -> CommandProfileRead:
    profile = await session.get(CommandProfile, profile_id)
    if profile is None:
        raise HTTPException(
            status_code=status.HTTP_404_NOT_FOUND,
            detail="Command profile not found",
        )
    profile.is_active = False
    await session.commit()
    await session.refresh(profile)
    return CommandProfileRead.model_validate(profile)
