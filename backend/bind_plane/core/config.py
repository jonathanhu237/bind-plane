from functools import lru_cache

from pydantic import AnyUrl, Field, model_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_file=".env",
        env_prefix="BIND_PLANE_",
        extra="ignore",
    )

    env: str = "development"
    secret_key: str = Field(default="change-me", min_length=1)
    credential_encryption_key: str = Field(default="change-me", min_length=1)
    database_url: str = "postgresql+asyncpg://bind_plane:bind_plane@localhost:5432/bind_plane"
    redis_url: str = "redis://localhost:6379/0"
    frontend_origin: AnyUrl | None = None

    @model_validator(mode="after")
    def reject_placeholder_secrets_outside_development(self) -> "Settings":
        if self.env == "development":
            return self

        placeholder_values = {"change-me", "changeme", "dev-secret", "development"}
        if self.secret_key in placeholder_values:
            raise ValueError("BIND_PLANE_SECRET_KEY must be set outside development")
        if self.credential_encryption_key in placeholder_values:
            raise ValueError(
                "BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY must be set outside development"
            )
        return self

    @property
    def debug(self) -> bool:
        return self.env == "development"


@lru_cache
def get_settings() -> Settings:
    return Settings()
