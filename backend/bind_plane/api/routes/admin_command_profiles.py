from typing import Literal
from uuid import UUID

from fastapi import APIRouter, HTTPException, Query, status
from sqlalchemy import or_, select

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.api.pagination import apply_sort, paginate_query
from bind_plane.db.models import CommandProfile
from bind_plane.schemas.admin import (
    CommandProfileCreate,
    CommandProfileRead,
    CommandProfileUpdate,
)
from bind_plane.schemas.pagination import PaginatedResponse

router = APIRouter(prefix="/admin/command-profiles", tags=["admin-command-profiles"])


@router.get("")
async def list_command_profiles(
    session: SessionDep,
    _: AdminUserDep,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=25, ge=1, le=200),
    search: str | None = Query(default=None, max_length=128),
    is_active: bool | None = Query(default=None),
    sort_by: str = Query(default="name", max_length=64),
    sort_order: Literal["asc", "desc"] = Query(default="asc"),
) -> PaginatedResponse[CommandProfileRead]:
    query = select(CommandProfile)
    if search:
        search_term = f"%{search.strip()}%"
        query = query.where(
            or_(
                CommandProfile.name.ilike(search_term),
                CommandProfile.description.ilike(search_term),
            )
        )
    if is_active is not None:
        query = query.where(CommandProfile.is_active == is_active)
    query = apply_sort(
        query,
        sort_by=sort_by,
        sort_order=sort_order,
        allowed={
            "name": CommandProfile.name,
            "created_at": CommandProfile.created_at,
            "updated_at": CommandProfile.updated_at,
            "is_active": CommandProfile.is_active,
        },
    )
    return await paginate_query(
        session,
        query,
        page=page,
        page_size=page_size,
        item_factory=CommandProfileRead.model_validate,
    )


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
