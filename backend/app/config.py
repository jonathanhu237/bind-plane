from pathlib import Path

from sqlalchemy.engine import URL
from pydantic_settings import BaseSettings

# .env file is at the project root (one level above backend/)
ENV_FILE = Path(__file__).resolve().parent.parent.parent / ".env"


class Settings(BaseSettings):
    model_config = {"env_file": ENV_FILE}

    # Server
    server_port: int = 8000

    # PostgreSQL
    postgres_host: str
    postgres_port: int = 5432
    postgres_user: str
    postgres_password: str
    postgres_db: str

    # Bootstrap admin (only used when no admin exists in the database)
    bootstrap_admin_username: str = "admin"
    bootstrap_admin_password: str = ""

    # Redis
    redis_host: str
    redis_port: int = 6379
    redis_password: str
    redis_db: int = 0

    # Session
    session_expires_hours: int = 336

    @property
    def database_url(self) -> URL:
        return URL.create(
            drivername="postgresql+asyncpg",
            username=self.postgres_user,
            password=self.postgres_password,
            host=self.postgres_host,
            port=self.postgres_port,
            database=self.postgres_db,
        )


settings = Settings()
