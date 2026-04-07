from dataclasses import dataclass

from passlib.context import CryptContext
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models.user import User
from app.session import SessionResult, SessionStore

pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")


class AuthenticationError(Exception):
    """Raised when login credentials are invalid."""


class InactiveUserError(Exception):
    """Raised when the user account is disabled."""


@dataclass
class LoginResult:
    user: User
    session: SessionResult


@dataclass
class AuthenticateResult:
    user: User
    expires_in: int


async def login(
    username: str,
    password: str,
    db: AsyncSession,
    session_store: SessionStore,
) -> LoginResult:
    """Validate credentials, create a session, and return the result."""
    result = await db.execute(
        select(User).where(User.username == username).limit(1)
    )
    user = result.scalar_one_or_none()

    if user is None or not pwd_context.verify(password, user.hashed_password):
        raise AuthenticationError("Invalid username or password")

    if not user.is_active:
        raise InactiveUserError("User account is disabled")

    session = await session_store.create(str(user.id))
    return LoginResult(user=user, session=session)


async def authenticate(
    session_id: str,
    db: AsyncSession,
    session_store: SessionStore,
) -> AuthenticateResult:
    """Validate a session and return the authenticated user."""
    user_id = await session_store.get(session_id)
    if user_id is None:
        raise AuthenticationError("Invalid or expired session")

    expires_in = await session_store.refresh(session_id)

    result = await db.execute(select(User).where(User.id == user_id).limit(1))
    user = result.scalar_one_or_none()

    if user is None or not user.is_active:
        await session_store.delete(session_id)
        raise AuthenticationError("User not found or inactive")

    return AuthenticateResult(user=user, expires_in=expires_in)


async def logout(session_id: str, session_store: SessionStore) -> None:
    """Delete the session from Redis."""
    await session_store.delete(session_id)
