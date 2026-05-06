import pytest
from bind_plane.core.config import Settings
from bind_plane.db.models import RoleName, User, UserRole
from bind_plane.security.passwords import hash_password, verify_password
from bind_plane.services.bootstrap import (
    InitialAdminBootstrapError,
    bootstrap_initial_admin,
)
from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker, create_async_engine
from sqlalchemy.orm import selectinload
from sqlalchemy.pool import StaticPool


async def test_bootstrap_creates_initial_admin(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    settings = Settings(
        initial_admin_username="admin",
        initial_admin_password="password123",
        initial_admin_display_name="Local Admin",
    )

    await bootstrap_initial_admin(settings, session_factory)

    async with session_factory() as session:
        result = await session.execute(select(User).options(selectinload(User.roles)))
        user = result.scalar_one()
        assert user.username == "admin"
        assert user.display_name == "Local Admin"
        assert user.is_active is True
        assert user.must_change_password is False
        assert verify_password("password123", user.password_hash)
        assert user.password_hash != "password123"
        assert user.roles[0].role == RoleName.ADMIN


async def test_bootstrap_does_not_override_existing_admin(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        admin = User(
            username="existing",
            password_hash=hash_password("old-password"),
            display_name="Existing Admin",
            must_change_password=False,
        )
        admin.roles = [UserRole(role=RoleName.ADMIN)]
        session.add(admin)
        await session.commit()

    settings = Settings(
        initial_admin_username="admin",
        initial_admin_password="password123",
        initial_admin_display_name="Local Admin",
    )

    await bootstrap_initial_admin(settings, session_factory)

    async with session_factory() as session:
        users = (await session.scalars(select(User).order_by(User.username))).all()
        assert [user.username for user in users] == ["existing"]
        assert users[0].display_name == "Existing Admin"
        assert verify_password("old-password", users[0].password_hash)


@pytest.mark.parametrize(
    ("username", "password", "message"),
    [
        (None, "password123", "BIND_PLANE_INITIAL_ADMIN_USERNAME"),
        ("admin", None, "BIND_PLANE_INITIAL_ADMIN_PASSWORD"),
        ("admin", "short", "at least 8 characters"),
    ],
)
async def test_bootstrap_requires_complete_valid_settings(
    session_factory: async_sessionmaker[AsyncSession],
    username: str | None,
    password: str | None,
    message: str,
) -> None:
    settings = Settings(initial_admin_username=username, initial_admin_password=password)

    with pytest.raises(InitialAdminBootstrapError, match=message):
        await bootstrap_initial_admin(settings, session_factory)


async def test_bootstrap_fails_when_username_belongs_to_non_admin(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        user = User(
            username="admin",
            password_hash=hash_password("password123"),
            display_name="Operator",
            must_change_password=False,
        )
        user.roles = [UserRole(role=RoleName.OPERATOR)]
        session.add(user)
        await session.commit()

    settings = Settings(initial_admin_username="admin", initial_admin_password="password123")

    with pytest.raises(InitialAdminBootstrapError, match="already belongs"):
        await bootstrap_initial_admin(settings, session_factory)

    async with session_factory() as session:
        user = await session.scalar(
            select(User).options(selectinload(User.roles)).where(User.username == "admin")
        )
        assert user is not None
        assert len(user.roles) == 1
        assert user.roles[0].role == RoleName.OPERATOR


async def test_bootstrap_reports_missing_migrations() -> None:
    engine = create_async_engine(
        "sqlite+aiosqlite://",
        connect_args={"check_same_thread": False},
        poolclass=StaticPool,
    )
    factory = async_sessionmaker(engine, expire_on_commit=False)
    settings = Settings(initial_admin_username="admin", initial_admin_password="password123")

    try:
        with pytest.raises(InitialAdminBootstrapError, match="alembic upgrade head"):
            await bootstrap_initial_admin(settings, factory)
    finally:
        await engine.dispose()


async def test_bootstrap_ignores_missing_initial_admin_settings_when_admin_exists(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        admin = User(
            username="existing",
            password_hash=hash_password("old-password"),
            must_change_password=False,
        )
        admin.roles = [UserRole(role=RoleName.ADMIN)]
        session.add(admin)
        await session.commit()

    await bootstrap_initial_admin(Settings(), session_factory)
