from dataclasses import dataclass
from enum import StrEnum
from ipaddress import IPv4Address
from uuid import UUID

from bind_plane.db.models import ArpEntryType


class ReleaseReason(StrEnum):
    TEMPORARY_TEST = "temporary_test"
    USER_REPORT = "user_report"
    IP_MAC_CHANGE = "ip_mac_change"
    WRONG_BINDING_FIX = "wrong_binding_fix"
    SECURITY_RESPONSE = "security_response"
    OTHER = "other"


class ReleasePreparationStatus(StrEnum):
    QUERY_QUEUED = "query_queued"
    READY = "ready"
    STOPPED_NO_RECORD = "stopped_no_record"
    STOPPED_NO_SWITCH = "stopped_no_switch"
    STOPPED_AMBIGUOUS_SWITCH = "stopped_ambiguous_switch"
    NEEDS_MANUAL_CONFIRMATION = "needs_manual_confirmation"


@dataclass(frozen=True)
class ArpObservation:
    target_ip: IPv4Address
    entry_type: ArpEntryType
    mac: str | None
    raw_output: str


@dataclass(frozen=True)
class ResolvedSwitch:
    switch_id: UUID
    network_id: UUID | None
    command_profile_id: UUID
    management_ip: str
    name: str
    cidr: str | None
    prefix_length: int | None


@dataclass(frozen=True)
class ReleasePreparation:
    status: ReleasePreparationStatus
    target_ip: IPv4Address
    resolved_switch: ResolvedSwitch | None
    observation: ArpObservation | None
    force: bool
    reason: ReleaseReason | None = None
