from collections.abc import Callable
from typing import Annotated
from uuid import UUID

from fastapi import Depends, HTTPException, status
from fastapi.security import HTTPAuthorizationCredentials, HTTPBearer
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import selectinload

from bind_plane.db.models import RoleName, User
from bind_plane.db.session import get_session
from bind_plane.security.tokens import TokenError, verify_access_token
from bind_plane.worker.queue import enqueue_pre_release_query_job, enqueue_release_job

SessionDep = Annotated[AsyncSession, Depends(get_session)]

bearer_scheme = HTTPBearer(auto_error=False)
BearerTokenDep = Annotated[HTTPAuthorizationCredentials | None, Depends(bearer_scheme)]


async def get_current_user(
    session: SessionDep,
    credentials: BearerTokenDep,
) -> User:
    if credentials is None:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Authentication required",
        )

    try:
        user_id = UUID(verify_access_token(credentials.credentials))
    except (TokenError, ValueError) as exc:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid token",
        ) from exc

    result = await session.execute(
        select(User).options(selectinload(User.roles)).where(User.id == user_id)
    )
    user = result.scalar_one_or_none()
    if user is None or not user.is_active:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Inactive or missing user",
        )
    return user


CurrentUserDep = Annotated[User, Depends(get_current_user)]


def require_role(*allowed_roles: RoleName) -> Callable[[CurrentUserDep], User]:
    async def dependency(current_user: CurrentUserDep) -> User:
        user_roles = {RoleName(role.role) for role in current_user.roles}
        if not user_roles.intersection(allowed_roles):
            raise HTTPException(
                status_code=status.HTTP_403_FORBIDDEN,
                detail="Insufficient permissions",
            )
        return current_user

    return dependency


OperatorUserDep = Annotated[User, Depends(require_role(RoleName.OPERATOR, RoleName.ADMIN))]
AdminUserDep = Annotated[User, Depends(require_role(RoleName.ADMIN))]

ReleaseEnqueuer = Callable[[UUID], None]


def get_release_enqueuer() -> ReleaseEnqueuer:
    return enqueue_release_job


ReleaseEnqueuerDep = Annotated[ReleaseEnqueuer, Depends(get_release_enqueuer)]


def get_pre_release_enqueuer() -> ReleaseEnqueuer:
    return enqueue_pre_release_query_job


PreReleaseEnqueuerDep = Annotated[ReleaseEnqueuer, Depends(get_pre_release_enqueuer)]
