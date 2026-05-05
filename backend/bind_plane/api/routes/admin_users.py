from fastapi import APIRouter, HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import selectinload

from bind_plane.api.deps import AdminUserDep, SessionDep
from bind_plane.db.models import RoleName, User, UserRole
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
) -> list[UserRead]:
    result = await session.execute(select(User).options(selectinload(User.roles)))
    return [UserRead.model_validate(user) for user in result.scalars().all()]


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
