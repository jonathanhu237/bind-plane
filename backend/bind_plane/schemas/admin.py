from datetime import datetime
from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field

from bind_plane.db.models import ImportBatchStatus


class CredentialCreate(BaseModel):
    name: str = Field(min_length=1, max_length=128)
    username: str = Field(min_length=1, max_length=128)
    password: str = Field(min_length=1)
    secret: str | None = None
    description: str | None = None
    is_active: bool = True


class CredentialUpdate(BaseModel):
    name: str | None = Field(default=None, min_length=1, max_length=128)
    username: str | None = Field(default=None, min_length=1, max_length=128)
    password: str | None = Field(default=None, min_length=1)
    secret: str | None = None
    description: str | None = None
    is_active: bool | None = None


class CredentialRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    name: str
    username: str
    description: str | None
    is_active: bool
    created_at: datetime
    updated_at: datetime


class CommandProfileCreate(BaseModel):
    name: str = Field(min_length=1, max_length=128)
    description: str | None = None
    login_prompt_patterns: dict[str, Any] = Field(default_factory=dict)
    command_templates: dict[str, Any] = Field(default_factory=dict)
    prompt_patterns: dict[str, Any] = Field(default_factory=dict)
    pagination_rules: dict[str, Any] = Field(default_factory=dict)
    success_patterns: list[str] = Field(default_factory=list)
    error_patterns: list[str] = Field(default_factory=list)
    parser_rules: dict[str, Any] = Field(default_factory=dict)
    is_active: bool = True


class CommandProfileUpdate(BaseModel):
    name: str | None = Field(default=None, min_length=1, max_length=128)
    description: str | None = None
    login_prompt_patterns: dict[str, Any] | None = None
    command_templates: dict[str, Any] | None = None
    prompt_patterns: dict[str, Any] | None = None
    pagination_rules: dict[str, Any] | None = None
    success_patterns: list[str] | None = None
    error_patterns: list[str] | None = None
    parser_rules: dict[str, Any] | None = None
    is_active: bool | None = None


class CommandProfileRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    name: str
    description: str | None
    login_prompt_patterns: dict[str, Any]
    command_templates: dict[str, Any]
    prompt_patterns: dict[str, Any]
    pagination_rules: dict[str, Any]
    success_patterns: list[str]
    error_patterns: list[str]
    parser_rules: dict[str, Any]
    is_active: bool
    created_at: datetime
    updated_at: datetime


class NetworkRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    cidr: str
    prefix_length: int
    vlan: str | None
    description: str | None
    is_enabled: bool
    is_validated: bool


class SwitchRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    name: str
    management_ip: str
    vendor: str | None
    model: str | None
    location: str | None
    credential_id: UUID
    command_profile_id: UUID
    is_enabled: bool
    networks: list[NetworkRead] = Field(default_factory=list)
    created_at: datetime
    updated_at: datetime


class SwitchNetworkImportRecord(BaseModel):
    switch_name: str = Field(min_length=1, max_length=128)
    management_ip: str = Field(min_length=1, max_length=45)
    cidr: str = Field(min_length=1, max_length=64)
    credential_id: UUID
    command_profile_id: UUID
    vendor: str | None = Field(default=None, max_length=128)
    model: str | None = Field(default=None, max_length=128)
    location: str | None = Field(default=None, max_length=255)
    vlan: str | None = Field(default=None, max_length=64)
    description: str | None = None
    switch_enabled: bool = True
    network_enabled: bool = True
    network_validated: bool = True


class SwitchNetworkImportRequest(BaseModel):
    source_filename: str | None = Field(default=None, max_length=255)
    records: list[SwitchNetworkImportRecord] = Field(min_length=1)


class ImportIssueRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    row_number: int | None
    severity: str
    message: str
    payload: dict[str, Any]
    created_at: datetime


class ImportBatchRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    kind: str
    source_filename: str | None
    status: ImportBatchStatus
    summary: dict[str, Any]
    issues: list[ImportIssueRead] = Field(default_factory=list)
    created_at: datetime
    updated_at: datetime
