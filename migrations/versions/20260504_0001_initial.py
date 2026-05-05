"""Initial bind-plane schema.

Revision ID: 20260504_0001
Revises:
Create Date: 2026-05-04 00:00:00.000000
"""

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "20260504_0001"
down_revision: str | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    op.create_table(
        "users",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("username", sa.String(length=128), nullable=False),
        sa.Column("password_hash", sa.Text(), nullable=False),
        sa.Column("display_name", sa.String(length=255), nullable=True),
        sa.Column("is_active", sa.Boolean(), nullable=False),
        sa.Column("must_change_password", sa.Boolean(), nullable=False),
        sa.Column("created_by_id", sa.UUID(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["created_by_id"], ["users.id"]),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(op.f("ix_users_username"), "users", ["username"], unique=True)

    op.create_table(
        "command_profiles",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("name", sa.String(length=128), nullable=False),
        sa.Column("description", sa.Text(), nullable=True),
        sa.Column("login_prompt_patterns", sa.JSON(), nullable=False),
        sa.Column("command_templates", sa.JSON(), nullable=False),
        sa.Column("prompt_patterns", sa.JSON(), nullable=False),
        sa.Column("pagination_rules", sa.JSON(), nullable=False),
        sa.Column("success_patterns", sa.JSON(), nullable=False),
        sa.Column("error_patterns", sa.JSON(), nullable=False),
        sa.Column("parser_rules", sa.JSON(), nullable=False),
        sa.Column("is_active", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("name"),
    )

    op.create_table(
        "credentials",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("name", sa.String(length=128), nullable=False),
        sa.Column("username", sa.String(length=128), nullable=False),
        sa.Column("encrypted_password", sa.Text(), nullable=False),
        sa.Column("encrypted_secret", sa.Text(), nullable=True),
        sa.Column("is_active", sa.Boolean(), nullable=False),
        sa.Column("description", sa.Text(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("name"),
    )

    op.create_table(
        "user_roles",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("user_id", sa.UUID(), nullable=False),
        sa.Column("role", sa.String(length=32), nullable=False),
        sa.ForeignKeyConstraint(["user_id"], ["users.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("user_id", "role", name="uq_user_roles_user_id_role"),
    )

    op.create_table(
        "switches",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("name", sa.String(length=128), nullable=False),
        sa.Column("management_ip", sa.String(length=45), nullable=False),
        sa.Column("vendor", sa.String(length=128), nullable=True),
        sa.Column("model", sa.String(length=128), nullable=True),
        sa.Column("location", sa.String(length=255), nullable=True),
        sa.Column("credential_id", sa.UUID(), nullable=False),
        sa.Column("command_profile_id", sa.UUID(), nullable=False),
        sa.Column("is_enabled", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["command_profile_id"], ["command_profiles.id"]),
        sa.ForeignKeyConstraint(["credential_id"], ["credentials.id"]),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(op.f("ix_switches_management_ip"), "switches", ["management_ip"], unique=True)

    op.create_table(
        "import_batches",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("kind", sa.String(length=64), nullable=False),
        sa.Column("source_filename", sa.String(length=255), nullable=True),
        sa.Column("status", sa.String(length=32), nullable=False),
        sa.Column("summary", sa.JSON(), nullable=False),
        sa.Column("created_by_id", sa.UUID(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["created_by_id"], ["users.id"]),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "audit_logs",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("actor_id", sa.UUID(), nullable=False),
        sa.Column("action", sa.String(length=128), nullable=False),
        sa.Column("target_type", sa.String(length=128), nullable=False),
        sa.Column("target_id", sa.UUID(), nullable=True),
        sa.Column("payload", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["actor_id"], ["users.id"]),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_audit_logs_actor_created", "audit_logs", ["actor_id", "created_at"])

    op.create_table(
        "networks",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("switch_id", sa.UUID(), nullable=False),
        sa.Column("cidr", sa.String(length=64), nullable=False),
        sa.Column("prefix_length", sa.Integer(), nullable=False),
        sa.Column("vlan", sa.String(length=64), nullable=True),
        sa.Column("description", sa.Text(), nullable=True),
        sa.Column("is_enabled", sa.Boolean(), nullable=False),
        sa.Column("is_validated", sa.Boolean(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["switch_id"], ["switches.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index("ix_networks_enabled_validated", "networks", ["is_enabled", "is_validated"])
    op.create_index("ix_networks_prefix", "networks", ["prefix_length"])

    op.create_table(
        "import_issues",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("batch_id", sa.UUID(), nullable=False),
        sa.Column("row_number", sa.Integer(), nullable=True),
        sa.Column("severity", sa.String(length=32), nullable=False),
        sa.Column("message", sa.Text(), nullable=False),
        sa.Column("payload", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["batch_id"], ["import_batches.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "release_jobs",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("target_ip", sa.String(length=15), nullable=False),
        sa.Column("kind", sa.String(length=32), nullable=False),
        sa.Column("reason", sa.String(length=128), nullable=False),
        sa.Column("ticket_id", sa.String(length=128), nullable=True),
        sa.Column("force", sa.Boolean(), nullable=False),
        sa.Column("status", sa.String(length=64), nullable=False),
        sa.Column("phase", sa.String(length=64), nullable=False),
        sa.Column("before_state", sa.JSON(), nullable=False),
        sa.Column("after_state", sa.JSON(), nullable=False),
        sa.Column("result", sa.JSON(), nullable=False),
        sa.Column("raw_before_output", sa.Text(), nullable=True),
        sa.Column("raw_release_output", sa.Text(), nullable=True),
        sa.Column("raw_after_output", sa.Text(), nullable=True),
        sa.Column("error_message", sa.Text(), nullable=True),
        sa.Column("started_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("finished_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("operator_id", sa.UUID(), nullable=False),
        sa.Column("switch_id", sa.UUID(), nullable=False),
        sa.Column("command_profile_id", sa.UUID(), nullable=False),
        sa.Column("retry_of_id", sa.UUID(), nullable=True),
        sa.Column("preparation_job_id", sa.UUID(), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["command_profile_id"], ["command_profiles.id"]),
        sa.ForeignKeyConstraint(["operator_id"], ["users.id"]),
        sa.ForeignKeyConstraint(["preparation_job_id"], ["release_jobs.id"]),
        sa.ForeignKeyConstraint(["retry_of_id"], ["release_jobs.id"]),
        sa.ForeignKeyConstraint(["switch_id"], ["switches.id"]),
        sa.PrimaryKeyConstraint("id"),
        sa.UniqueConstraint("preparation_job_id", name="uq_release_jobs_preparation_job_id"),
    )
    op.create_index(
        "ix_release_jobs_operator_created",
        "release_jobs",
        ["operator_id", "created_at"],
    )
    op.create_index("ix_release_jobs_target_active", "release_jobs", ["target_ip", "status"])

    op.create_table(
        "release_job_events",
        sa.Column("id", sa.UUID(), nullable=False),
        sa.Column("job_id", sa.UUID(), nullable=False),
        sa.Column("phase", sa.String(length=64), nullable=False),
        sa.Column("status", sa.String(length=64), nullable=False),
        sa.Column("message", sa.Text(), nullable=True),
        sa.Column("payload", sa.JSON(), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["job_id"], ["release_jobs.id"], ondelete="CASCADE"),
        sa.PrimaryKeyConstraint("id"),
    )
    op.create_index(
        "ix_release_job_events_job_created",
        "release_job_events",
        ["job_id", "created_at"],
    )


def downgrade() -> None:
    op.drop_index("ix_release_job_events_job_created", table_name="release_job_events")
    op.drop_table("release_job_events")
    op.drop_index("ix_release_jobs_target_active", table_name="release_jobs")
    op.drop_index("ix_release_jobs_operator_created", table_name="release_jobs")
    op.drop_table("release_jobs")
    op.drop_table("import_issues")
    op.drop_index("ix_networks_prefix", table_name="networks")
    op.drop_index("ix_networks_enabled_validated", table_name="networks")
    op.drop_table("networks")
    op.drop_index("ix_audit_logs_actor_created", table_name="audit_logs")
    op.drop_table("audit_logs")
    op.drop_table("import_batches")
    op.drop_index(op.f("ix_switches_management_ip"), table_name="switches")
    op.drop_table("switches")
    op.drop_table("user_roles")
    op.drop_table("credentials")
    op.drop_table("command_profiles")
    op.drop_index(op.f("ix_users_username"), table_name="users")
    op.drop_table("users")
