from fastapi import APIRouter, HTTPException, status
from sqlalchemy import select
from sqlalchemy.orm import selectinload

from bind_plane.api.deps import CurrentUserDep, SessionDep
from bind_plane.db.models import User
from bind_plane.schemas.auth import LoginRequest, TokenResponse
from bind_plane.schemas.users import UserRead
from bind_plane.security.passwords import verify_password
from bind_plane.security.tokens import create_access_token

router = APIRouter(prefix="/auth", tags=["auth"])


@router.post("/login")
async def login(payload: LoginRequest, session: SessionDep) -> TokenResponse:
    result = await session.execute(
        select(User).options(selectinload(User.roles)).where(User.username == payload.username)
    )
    user = result.scalar_one_or_none()
    password_valid = user is not None and verify_password(payload.password, user.password_hash)
    if user is None or not user.is_active or not password_valid:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid username or password",
        )

    return TokenResponse(access_token=create_access_token(str(user.id)))


@router.get("/me")
async def me(current_user: CurrentUserDep) -> UserRead:
    return UserRead.model_validate(current_user)
