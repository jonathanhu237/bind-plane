from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field, model_validator

from bind_plane.db.models import RoleName


class UserCreate(BaseModel):
    username: str = Field(min_length=1, max_length=128)
    password: str = Field(min_length=8)
    display_name: str | None = Field(default=None, max_length=255)
    roles: list[RoleName] = Field(min_length=1)


class UserResetPassword(BaseModel):
    password: str = Field(min_length=8)


class UserUpdate(BaseModel):
    display_name: str | None = Field(default=None, max_length=255)
    is_active: bool | None = None
    must_change_password: bool | None = None
    roles: list[RoleName] | None = None


class UserRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    username: str
    display_name: str | None
    is_active: bool
    must_change_password: bool
    roles: list[RoleName]

    @model_validator(mode="before")
    @classmethod
    def flatten_roles(cls, value: Any) -> Any:
        if hasattr(value, "roles"):
            return {
                "id": value.id,
                "username": value.username,
                "display_name": value.display_name,
                "is_active": value.is_active,
                "must_change_password": value.must_change_password,
                "roles": [role.role for role in value.roles],
            }
        return value
