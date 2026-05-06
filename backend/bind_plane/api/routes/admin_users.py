from typing import Literal

from fastapi import APIRouter, HTTPException, Query, status
from sqlalchemy import or_, select
from sqlalchemy.orm import selectinload

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.api.pagination import apply_sort, paginate_query
from bind_plane.db.models import RoleName, User, UserRole
from bind_plane.schemas.pagination import PaginatedResponse
from bind_plane.schemas.users import UserCreate, UserRead, UserResetPassword, UserUpdate
from bind_plane.security.passwords import hash_password

router = APIRouter(prefix="/admin/users", tags=["admin-users"])


@router.post("")
async def create_user(
    payload: UserCreate,
    session: SessionDep,
    current_admin: AdminUserDep,
) -> UserRead:
    existing = await session.scalar(select(User).where(User.username == payload.username))
    if existing is not None:
        raise HTTPException(
            status_code=status.HTTP_409_CONFLICT,
            detail="Username already exists",
        )

    user = User(
        username=payload.username,
        display_name=payload.display_name,
        password_hash=hash_password(payload.password),
        must_change_password=True,
        created_by_id=current_admin.id,
    )
    user.roles = [UserRole(role=role) for role in payload.roles]
    session.add(user)
    await session.commit()
    await session.refresh(user, attribute_names=["roles"])
    return UserRead.model_validate(user)


@router.post("/{username}/reset-password")
async def reset_password(
    username: str,
    payload: UserResetPassword,
    session: SessionDep,
    current_admin: AdminUserDep,
) -> UserRead:
    result = await session.execute(
        select(User).options(selectinload(User.roles)).where(User.username == username)
    )
    user = result.scalar_one_or_none()
    if user is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="User not found")

    user.password_hash = hash_password(payload.password)
    user.must_change_password = True
    user.created_by_id = current_admin.id
    await session.commit()
    await session.refresh(user, attribute_names=["roles"])
    return UserRead.model_validate(user)


@router.get("")
async def list_users(
    session: SessionDep,
    _: AdminUserDep,
    page: int = Query(default=1, ge=1),
    page_size: int = Query(default=25, ge=1, le=200),
    search: str | None = Query(default=None, max_length=128),
    role: RoleName | None = None,
    is_active: bool | None = None,
    sort_by: str = Query(default="username", max_length=64),
    sort_order: Literal["asc", "desc"] = Query(default="asc"),
) -> PaginatedResponse[UserRead]:
    query = select(User).options(selectinload(User.roles))
    if search:
        search_term = f"%{search.strip()}%"
        query = query.where(
            or_(
                User.username.ilike(search_term),
                User.display_name.ilike(search_term),
            )
        )
    if role is not None:
        query = query.where(User.roles.any(UserRole.role == role))
    if is_active is not None:
        query = query.where(User.is_active == is_active)
    query = apply_sort(
        query,
        sort_by=sort_by,
        sort_order=sort_order,
        allowed={
            "username": User.username,
            "created_at": User.created_at,
            "updated_at": User.updated_at,
            "is_active": User.is_active,
        },
    )
    return await paginate_query(
        session,
        query,
        page=page,
        page_size=page_size,
        item_factory=UserRead.model_validate,
    )


@router.patch("/{username}")
async def update_user(
    username: str,
    payload: UserUpdate,
    session: SessionDep,
    _: AdminUserDep,
) -> UserRead:
    result = await session.execute(
        select(User).options(selectinload(User.roles)).where(User.username == username)
    )
    user = result.scalar_one_or_none()
    if user is None:
        raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="User not found")

    fields = payload.model_fields_set
    if "display_name" in fields:
        user.display_name = payload.display_name
    if "is_active" in fields and payload.is_active is not None:
        user.is_active = payload.is_active
    if "must_change_password" in fields and payload.must_change_password is not None:
        user.must_change_password = payload.must_change_password
    if "roles" in fields and payload.roles is not None:
        user.roles = [UserRole(role=role) for role in payload.roles]

    await session.commit()
    await session.refresh(user, attribute_names=["roles"])
    return UserRead.model_validate(user)


@router.get("/roles")
async def list_roles(_: AdminUserDep) -> list[RoleName]:
    return [RoleName.OPERATOR, RoleName.ADMIN]
