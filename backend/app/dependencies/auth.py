from fastapi import Cookie, Depends, HTTPException, status
from sqlalchemy.ext.asyncio import AsyncSession

from app.database import get_db
from app.models.user import User
from app.redis import get_redis
from app.services.auth import AuthenticationError, authenticate
from app.session import SessionStore


def get_session_store() -> SessionStore:
    """FastAPI dependency that returns a SessionStore instance."""
    return SessionStore(get_redis())


async def get_current_user(
    session_id: str | None = Cookie(default=None),
    db: AsyncSession = Depends(get_db),
    session_store: SessionStore = Depends(get_session_store),
) -> User:
    """FastAPI dependency that returns the current authenticated user."""
    if session_id is None:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Not authenticated",
        )

    try:
        result = await authenticate(session_id, db, session_store)
    except AuthenticationError:
        raise HTTPException(
            status_code=status.HTTP_401_UNAUTHORIZED,
            detail="Invalid or expired session",
        )

    return result.user


async def require_admin(
    user: User = Depends(get_current_user),
) -> User:
    """FastAPI dependency that requires the current user to be an admin."""
    if not user.is_admin:
        raise HTTPException(
            status_code=status.HTTP_403_FORBIDDEN,
            detail="Admin access required",
        )
    return user
