from datetime import UTC, datetime
from enum import StrEnum
from uuid import UUID, uuid4

from sqlalchemy import (
    JSON,
    Boolean,
    DateTime,
    ForeignKey,
    Index,
    Integer,
    String,
    Text,
    UniqueConstraint,
)
from sqlalchemy.orm import Mapped, mapped_column, relationship

from bind_plane.db.base import Base


def utc_now() -> datetime:
    return datetime.now(UTC)


class RoleName(StrEnum):
    OPERATOR = "operator"
    ADMIN = "admin"


class ReleaseJobStatus(StrEnum):
    QUEUED = "queued"
    RUNNING = "running"
    WAITING_CONFIRMATION = "waiting_confirmation"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    TIMEOUT = "timeout"
    NEEDS_MANUAL_CONFIRMATION = "needs_manual_confirmation"
    CANCELLED = "cancelled"


class ReleaseJobPhase(StrEnum):
    QUEUED = "queued"
    RESOLVING_SWITCH = "resolving_switch"
    QUERYING_BEFORE = "querying_before"
    WAITING_CONFIRMATION = "waiting_confirmation"
    CONNECTING = "connecting"
    ENTERING_CONFIG = "entering_config"
    RELEASING = "releasing"
    QUERYING_AFTER = "querying_after"
    CLASSIFYING = "classifying"
    FINISHED = "finished"


class ReleaseJobKind(StrEnum):
    PRE_RELEASE_QUERY = "pre_release_query"
    RELEASE = "release"


class ArpEntryType(StrEnum):
    STATIC = "static"
    DYNAMIC = "dynamic"
    MISSING = "missing"
    UNKNOWN = "unknown"


class ImportBatchStatus(StrEnum):
    DRAFT = "draft"
    VALIDATED = "validated"
    FAILED = "failed"
    APPLIED = "applied"


class TimestampMixin:
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=utc_now,
        nullable=False,
    )
    updated_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=utc_now,
        onupdate=utc_now,
        nullable=False,
    )


class User(Base, TimestampMixin):
    __tablename__ = "users"

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    username: Mapped[str] = mapped_column(String(128), nullable=False, unique=True, index=True)
    password_hash: Mapped[str] = mapped_column(Text, nullable=False)
    display_name: Mapped[str | None] = mapped_column(String(255))
    is_active: Mapped[bool] = mapped_column(Boolean, nullable=False, default=True)
    must_change_password: Mapped[bool] = mapped_column(Boolean, nullable=False, default=True)
    created_by_id: Mapped[UUID | None] = mapped_column(ForeignKey("users.id"))

    roles: Mapped[list["UserRole"]] = relationship(
        back_populates="user",
        cascade="all, delete-orphan",
    )
    release_jobs: Mapped[list["ReleaseJob"]] = relationship(
        back_populates="operator",
        foreign_keys="ReleaseJob.operator_id",
    )
    created_by: Mapped["User | None"] = relationship(
        remote_side="User.id",
        back_populates="created_users",
    )
    created_users: Mapped[list["User"]] = relationship(back_populates="created_by")


class UserRole(Base):
    __tablename__ = "user_roles"
    __table_args__ = (UniqueConstraint("user_id", "role", name="uq_user_roles_user_id_role"),)

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    user_id: Mapped[UUID] = mapped_column(
        ForeignKey("users.id", ondelete="CASCADE"),
        nullable=False,
    )
    role: Mapped[RoleName] = mapped_column(String(32), nullable=False)

    user: Mapped[User] = relationship(back_populates="roles")


class Credential(Base, TimestampMixin):
    __tablename__ = "credentials"

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    name: Mapped[str] = mapped_column(String(128), nullable=False, unique=True)
    username: Mapped[str] = mapped_column(String(128), nullable=False)
    encrypted_password: Mapped[str] = mapped_column(Text, nullable=False)
    encrypted_secret: Mapped[str | None] = mapped_column(Text)
    is_active: Mapped[bool] = mapped_column(Boolean, nullable=False, default=True)
    description: Mapped[str | None] = mapped_column(Text)

    switches: Mapped[list["Switch"]] = relationship(back_populates="credential")


class CommandProfile(Base, TimestampMixin):
    __tablename__ = "command_profiles"

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    name: Mapped[str] = mapped_column(String(128), nullable=False, unique=True)
    description: Mapped[str | None] = mapped_column(Text)
    login_prompt_patterns: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    command_templates: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    prompt_patterns: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    pagination_rules: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    success_patterns: Mapped[list] = mapped_column(JSON, nullable=False, default=list)
    error_patterns: Mapped[list] = mapped_column(JSON, nullable=False, default=list)
    parser_rules: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    is_active: Mapped[bool] = mapped_column(Boolean, nullable=False, default=True)

    switches: Mapped[list["Switch"]] = relationship(back_populates="command_profile")


class Switch(Base, TimestampMixin):
    __tablename__ = "switches"

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    name: Mapped[str] = mapped_column(String(128), nullable=False)
    management_ip: Mapped[str] = mapped_column(String(45), nullable=False, unique=True, index=True)
    vendor: Mapped[str | None] = mapped_column(String(128))
    model: Mapped[str | None] = mapped_column(String(128))
    location: Mapped[str | None] = mapped_column(String(255))
    credential_id: Mapped[UUID] = mapped_column(ForeignKey("credentials.id"), nullable=False)
    command_profile_id: Mapped[UUID] = mapped_column(
        ForeignKey("command_profiles.id"),
        nullable=False,
    )
    is_enabled: Mapped[bool] = mapped_column(Boolean, nullable=False, default=True)

    credential: Mapped[Credential] = relationship(back_populates="switches")
    command_profile: Mapped[CommandProfile] = relationship(back_populates="switches")
    networks: Mapped[list["Network"]] = relationship(
        back_populates="switch",
        cascade="all, delete-orphan",
    )


class Network(Base, TimestampMixin):
    __tablename__ = "networks"
    __table_args__ = (
        Index("ix_networks_enabled_validated", "is_enabled", "is_validated"),
        Index("ix_networks_prefix", "prefix_length"),
    )

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    switch_id: Mapped[UUID] = mapped_column(
        ForeignKey("switches.id", ondelete="CASCADE"),
        nullable=False,
    )
    cidr: Mapped[str] = mapped_column(String(64), nullable=False)
    prefix_length: Mapped[int] = mapped_column(Integer, nullable=False)
    vlan: Mapped[str | None] = mapped_column(String(64))
    description: Mapped[str | None] = mapped_column(Text)
    is_enabled: Mapped[bool] = mapped_column(Boolean, nullable=False, default=True)
    is_validated: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)

    switch: Mapped[Switch] = relationship(back_populates="networks")


class ImportBatch(Base, TimestampMixin):
    __tablename__ = "import_batches"

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    kind: Mapped[str] = mapped_column(String(64), nullable=False)
    source_filename: Mapped[str | None] = mapped_column(String(255))
    status: Mapped[ImportBatchStatus] = mapped_column(String(32), nullable=False)
    summary: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    created_by_id: Mapped[UUID] = mapped_column(ForeignKey("users.id"), nullable=False)

    issues: Mapped[list["ImportIssue"]] = relationship(
        back_populates="batch",
        cascade="all, delete-orphan",
    )


class ImportIssue(Base, TimestampMixin):
    __tablename__ = "import_issues"

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    batch_id: Mapped[UUID] = mapped_column(
        ForeignKey("import_batches.id", ondelete="CASCADE"),
        nullable=False,
    )
    row_number: Mapped[int | None] = mapped_column(Integer)
    severity: Mapped[str] = mapped_column(String(32), nullable=False)
    message: Mapped[str] = mapped_column(Text, nullable=False)
    payload: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)

    batch: Mapped[ImportBatch] = relationship(back_populates="issues")


class ReleaseJob(Base, TimestampMixin):
    __tablename__ = "release_jobs"
    __table_args__ = (
        Index("ix_release_jobs_target_active", "target_ip", "status"),
        Index("ix_release_jobs_operator_created", "operator_id", "created_at"),
        UniqueConstraint("preparation_job_id", name="uq_release_jobs_preparation_job_id"),
    )

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    target_ip: Mapped[str] = mapped_column(String(15), nullable=False)
    kind: Mapped[ReleaseJobKind] = mapped_column(
        String(32),
        nullable=False,
        default=ReleaseJobKind.RELEASE,
    )
    reason: Mapped[str] = mapped_column(String(128), nullable=False)
    ticket_id: Mapped[str | None] = mapped_column(String(128))
    force: Mapped[bool] = mapped_column(Boolean, nullable=False, default=False)
    status: Mapped[ReleaseJobStatus] = mapped_column(String(64), nullable=False)
    phase: Mapped[ReleaseJobPhase] = mapped_column(String(64), nullable=False)
    before_state: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    after_state: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    result: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    raw_before_output: Mapped[str | None] = mapped_column(Text)
    raw_release_output: Mapped[str | None] = mapped_column(Text)
    raw_after_output: Mapped[str | None] = mapped_column(Text)
    error_message: Mapped[str | None] = mapped_column(Text)
    started_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))
    finished_at: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))

    operator_id: Mapped[UUID] = mapped_column(ForeignKey("users.id"), nullable=False)
    switch_id: Mapped[UUID] = mapped_column(ForeignKey("switches.id"), nullable=False)
    command_profile_id: Mapped[UUID] = mapped_column(
        ForeignKey("command_profiles.id"),
        nullable=False,
    )
    retry_of_id: Mapped[UUID | None] = mapped_column(ForeignKey("release_jobs.id"))
    preparation_job_id: Mapped[UUID | None] = mapped_column(ForeignKey("release_jobs.id"))

    operator: Mapped[User] = relationship(back_populates="release_jobs", foreign_keys=[operator_id])
    switch: Mapped[Switch] = relationship()
    command_profile: Mapped[CommandProfile] = relationship()
    retry_of: Mapped["ReleaseJob | None"] = relationship(
        remote_side="ReleaseJob.id",
        foreign_keys=[retry_of_id],
    )
    preparation_job: Mapped["ReleaseJob | None"] = relationship(
        remote_side="ReleaseJob.id",
        foreign_keys=[preparation_job_id],
    )
    events: Mapped[list["ReleaseJobEvent"]] = relationship(
        back_populates="job",
        cascade="all, delete-orphan",
    )


class ReleaseJobEvent(Base):
    __tablename__ = "release_job_events"
    __table_args__ = (Index("ix_release_job_events_job_created", "job_id", "created_at"),)

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    job_id: Mapped[UUID] = mapped_column(
        ForeignKey("release_jobs.id", ondelete="CASCADE"),
        nullable=False,
    )
    phase: Mapped[ReleaseJobPhase] = mapped_column(String(64), nullable=False)
    status: Mapped[ReleaseJobStatus] = mapped_column(String(64), nullable=False)
    message: Mapped[str | None] = mapped_column(Text)
    payload: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=utc_now,
        nullable=False,
    )

    job: Mapped[ReleaseJob] = relationship(back_populates="events")


class AuditLog(Base):
    __tablename__ = "audit_logs"
    __table_args__ = (Index("ix_audit_logs_actor_created", "actor_id", "created_at"),)

    id: Mapped[UUID] = mapped_column(primary_key=True, default=uuid4)
    actor_id: Mapped[UUID] = mapped_column(ForeignKey("users.id"), nullable=False)
    action: Mapped[str] = mapped_column(String(128), nullable=False)
    target_type: Mapped[str] = mapped_column(String(128), nullable=False)
    target_id: Mapped[UUID | None] = mapped_column()
    payload: Mapped[dict] = mapped_column(JSON, nullable=False, default=dict)
    created_at: Mapped[datetime] = mapped_column(
        DateTime(timezone=True),
        default=utc_now,
        nullable=False,
    )

    actor: Mapped[User] = relationship()
