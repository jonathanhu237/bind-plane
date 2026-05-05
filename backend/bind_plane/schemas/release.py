from datetime import datetime
from typing import Any
from uuid import UUID

from pydantic import BaseModel, ConfigDict, Field

from bind_plane.db.models import ArpEntryType, ReleaseJobKind, ReleaseJobPhase, ReleaseJobStatus
from bind_plane.domain.release import ReleasePreparationStatus, ReleaseReason


class ReleasePrepareRequest(BaseModel):
    target_ip: str = Field(min_length=1)
    reason: ReleaseReason
    force: bool = False
    selected_switch_id: UUID | None = None


class ArpObservationRead(BaseModel):
    entry_type: ArpEntryType
    mac: str | None
    raw_output: str


class ResolvedSwitchRead(BaseModel):
    switch_id: UUID
    network_id: UUID | None
    command_profile_id: UUID
    management_ip: str
    name: str
    cidr: str | None
    prefix_length: int | None
    selection_source: str = "resolved_network"


class ReleasePreparationRead(BaseModel):
    preparation_job_id: UUID | None = None
    status: ReleasePreparationStatus
    target_ip: str
    resolved_switch: ResolvedSwitchRead | None
    observation: ArpObservationRead | None
    force: bool
    reason: ReleaseReason | None


class ReleaseJobCreateRequest(BaseModel):
    preparation_job_id: UUID
    target_ip: str = Field(min_length=1)
    reason: ReleaseReason
    ticket_id: str | None = Field(default=None, max_length=128)
    force: bool = False
    confirmed: bool = False


class ReleaseJobCreated(BaseModel):
    job_id: UUID
    status: ReleaseJobStatus
    phase: ReleaseJobPhase


class SwitchSummaryRead(BaseModel):
    id: UUID
    name: str
    management_ip: str


class OperatorSummaryRead(BaseModel):
    id: UUID
    username: str
    display_name: str | None


class RawReleaseOutputRead(BaseModel):
    before: str | None
    release: str | None
    after: str | None


class ReleaseJobEventRead(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: UUID
    phase: ReleaseJobPhase
    status: ReleaseJobStatus
    message: str | None
    payload: dict[str, Any]
    created_at: datetime


class ReleaseJobRead(BaseModel):
    id: UUID
    target_ip: str
    kind: ReleaseJobKind
    reason: ReleaseReason
    ticket_id: str | None
    force: bool
    status: ReleaseJobStatus
    phase: ReleaseJobPhase
    before_state: dict[str, Any]
    after_state: dict[str, Any]
    result: dict[str, Any]
    error_message: str | None
    operator: OperatorSummaryRead
    switch: SwitchSummaryRead
    retry_of_id: UUID | None
    preparation_job_id: UUID | None
    raw_output: RawReleaseOutputRead | None
    events: list[ReleaseJobEventRead]
    created_at: datetime
    updated_at: datetime
    started_at: datetime | None
    finished_at: datetime | None
