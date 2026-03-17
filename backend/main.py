import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI
from passlib.context import CryptContext
from sqlalchemy import select
from sqlalchemy.exc import IntegrityError

from app.config import settings
from app.database import async_session, engine
from app.models.user import Base, User

logger = logging.getLogger(__name__)
pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")


async def seed_admin() -> None:
    """Create the default admin user if no admin exists in the database."""
    if not settings.bootstrap_admin_password:
        logger.warning("BOOTSTRAP_ADMIN_PASSWORD is empty, skipping admin creation")
        return

    async with async_session() as session:
        admin_id = await session.scalar(select(User.id).where(User.is_admin.is_(True)).limit(1))
        if admin_id is not None:
            logger.info("Admin user already exists, skipping")
            return

        admin = User(
            username=settings.bootstrap_admin_username,
            hashed_password=pwd_context.hash(settings.bootstrap_admin_password),
            is_admin=True,
        )
        session.add(admin)
        try:
            await session.commit()
        except IntegrityError:
            await session.rollback()

            bootstrap_user = await session.scalar(
                select(User).where(User.username == settings.bootstrap_admin_username).limit(1)
            )
            if bootstrap_user is not None and bootstrap_user.is_admin:
                logger.info(
                    "Admin user %s was created concurrently, skipping",
                    settings.bootstrap_admin_username,
                )
                return

            admin_id = await session.scalar(select(User.id).where(User.is_admin.is_(True)).limit(1))
            if admin_id is not None:
                logger.info("Admin user was created concurrently, skipping bootstrap")
                return

            if bootstrap_user is not None:
                raise RuntimeError(
                    f"Cannot bootstrap admin user {settings.bootstrap_admin_username!r}: "
                    "username is already taken by a non-admin user."
                ) from None

            raise

        logger.info("Created default admin user: %s", settings.bootstrap_admin_username)


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup: create tables and seed admin
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
    await seed_admin()

    yield

    # Shutdown: dispose engine
    await engine.dispose()


app = FastAPI(title="BindPlane", description="IP/MAC binding operations platform", lifespan=lifespan)


@app.get("/")
async def root():
    return {"message": "BindPlane API is running"}
