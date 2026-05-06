import logging
from dataclasses import dataclass

from sqlalchemy import select
from sqlalchemy.exc import OperationalError, ProgrammingError
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker
from sqlalchemy.orm import selectinload

from bind_plane.core.config import Settings
from bind_plane.db.models import RoleName, User, UserRole
from bind_plane.db.session import async_session
from bind_plane.security.passwords import hash_password

logger = logging.getLogger(__name__)


class InitialAdminBootstrapError(RuntimeError):
    pass


@dataclass(frozen=True)
class InitialAdminSettings:
    username: str
    password: str
    display_name: str | None


def get_initial_admin_settings(settings: Settings) -> InitialAdminSettings:
    username = settings.initial_admin_username
    password = settings.initial_admin_password

    if username is None or username.strip() == "":
        raise InitialAdminBootstrapError(
            "BIND_PLANE_INITIAL_ADMIN_USERNAME is required when no admin exists"
        )
    if password is None or password == "":
        raise InitialAdminBootstrapError(
            "BIND_PLANE_INITIAL_ADMIN_PASSWORD is required when no admin exists"
        )
    if len(password) < 8:
        raise InitialAdminBootstrapError(
            "BIND_PLANE_INITIAL_ADMIN_PASSWORD must be at least 8 characters"
        )

    return InitialAdminSettings(
        username=username.strip(),
        password=password,
        display_name=settings.initial_admin_display_name,
    )


async def admin_exists(session: AsyncSession) -> bool:
    result = await session.execute(
        select(User.id).join(UserRole).where(UserRole.role == RoleName.ADMIN).limit(1)
    )
    return result.scalar_one_or_none() is not None


async def bootstrap_initial_admin(
    settings: Settings,
    session_factory: async_sessionmaker[AsyncSession] = async_session,
) -> None:
    try:
        async with session_factory() as session:
            if await admin_exists(session):
                return

            initial_admin = get_initial_admin_settings(settings)
            result = await session.execute(
                select(User)
                .options(selectinload(User.roles))
                .where(User.username == initial_admin.username)
            )
            existing_user = result.scalar_one_or_none()
            if existing_user is not None:
                raise InitialAdminBootstrapError(
                    "BIND_PLANE_INITIAL_ADMIN_USERNAME already belongs to a non-admin user"
                )

            user = User(
                username=initial_admin.username,
                display_name=initial_admin.display_name,
                password_hash=hash_password(initial_admin.password),
                must_change_password=False,
            )
            user.roles = [UserRole(role=RoleName.ADMIN)]
            session.add(user)
            await session.commit()
            logger.info(
                "Created initial admin user username=%s display_name=%s",
                initial_admin.username,
                initial_admin.display_name,
            )
    except (OperationalError, ProgrammingError) as exc:
        raise InitialAdminBootstrapError(
            "Database is not migrated; run `uv run alembic upgrade head` before starting FastAPI"
        ) from exc
