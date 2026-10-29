-- Device operations: inventory metadata, reachability, encrypted configuration
-- backups, compliance baselines, schedules, task runs and alerts.
CREATE TABLE bp_switch_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text UNIQUE NOT NULL,
    description text,
    version integer NOT NULL DEFAULT 1
);

-- Status columns are written by the worker and never bump the switch version:
-- only configuration that changes how a switch is reached pins release jobs.
ALTER TABLE bp_switches
    ADD COLUMN vendor text NOT NULL DEFAULT 'other' CHECK (vendor IN ('huawei', 'h3c', 'ruijie', 'cisco', 'other')),
    ADD COLUMN model text,
    ADD COLUMN site text,
    ADD COLUMN description text,
    ADD COLUMN group_id uuid REFERENCES bp_switch_groups(id) ON DELETE SET NULL,
    ADD COLUMN protocol text NOT NULL DEFAULT 'telnet' CHECK (protocol IN ('telnet', 'ssh')),
    ADD COLUMN port integer CHECK (port BETWEEN 1 AND 65535),
    ADD COLUMN reachable boolean,
    ADD COLUMN reachability_checked_at timestamptz,
    ADD COLUMN reachability_changed_at timestamptz,
    ADD COLUMN reachability_latency_ms integer,
    ADD COLUMN reachability_error text,
    ADD COLUMN config_checked_at timestamptz,
    ADD COLUMN config_error text;
CREATE INDEX bp_switches_group_idx ON bp_switches(group_id);

-- Configuration text holds secrets (hashed passwords, SNMP communities), so it is
-- sealed with AES-GCM by the worker and opened only by the API for admins.
CREATE TABLE bp_config_backups (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE RESTRICT,
    captured_at timestamptz NOT NULL DEFAULT now(),
    content_sha256 text NOT NULL,
    size_bytes integer NOT NULL,
    line_count integer NOT NULL,
    encrypted_content text NOT NULL
);
CREATE INDEX bp_config_backups_switch_idx ON bp_config_backups(switch_id, captured_at DESC, id DESC);

CREATE TABLE bp_compliance_rules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text UNIQUE NOT NULL,
    description text,
    pattern text NOT NULL,
    mode text NOT NULL CHECK (mode IN ('must_contain', 'must_not_contain')),
    severity text NOT NULL DEFAULT 'warning' CHECK (severity IN ('info', 'warning', 'critical')),
    vendor text CHECK (vendor IN ('huawei', 'h3c', 'ruijie', 'cisco', 'other')),
    group_id uuid REFERENCES bp_switch_groups(id) ON DELETE SET NULL,
    is_enabled boolean NOT NULL DEFAULT true,
    version integer NOT NULL DEFAULT 1
);

-- One current result per switch and rule; stale when the backup or rule version moves on.
CREATE TABLE bp_compliance_results (
    switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE CASCADE,
    rule_id uuid NOT NULL REFERENCES bp_compliance_rules(id) ON DELETE CASCADE,
    backup_id bigint NOT NULL REFERENCES bp_config_backups(id) ON DELETE CASCADE,
    rule_version integer NOT NULL,
    passed boolean NOT NULL,
    detail text,
    evaluated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (switch_id, rule_id)
);
CREATE INDEX bp_compliance_results_failed_idx ON bp_compliance_results(rule_id) WHERE NOT passed;

CREATE TABLE bp_schedules (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text UNIQUE NOT NULL,
    kind text NOT NULL CHECK (kind IN ('config_backup', 'reachability')),
    cron text NOT NULL,
    scope text NOT NULL CHECK (scope IN ('all', 'group')),
    group_id uuid REFERENCES bp_switch_groups(id) ON DELETE RESTRICT,
    is_enabled boolean NOT NULL DEFAULT true,
    next_run_at timestamptz,
    last_run_at timestamptz,
    version integer NOT NULL DEFAULT 1,
    CONSTRAINT bp_schedules_scope_group CHECK ((scope = 'group') = (group_id IS NOT NULL))
);
CREATE INDEX bp_schedules_due_idx ON bp_schedules(next_run_at) WHERE is_enabled;

CREATE TABLE bp_device_tasks (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    kind text NOT NULL CHECK (kind IN ('config_backup', 'reachability')),
    schedule_id uuid REFERENCES bp_schedules(id) ON DELETE SET NULL,
    actor_id uuid REFERENCES auth_users(id) ON DELETE SET NULL,
    actor_email text,
    scope_label text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bp_device_tasks_created_idx ON bp_device_tasks(created_at DESC);

-- Device operations only read from switches, so an expired lease simply fails the item.
CREATE TABLE bp_device_task_items (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    task_id uuid NOT NULL REFERENCES bp_device_tasks(id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('config_backup', 'reachability')),
    switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE CASCADE,
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'skipped')),
    message text,
    changed boolean,
    backup_id bigint REFERENCES bp_config_backups(id) ON DELETE SET NULL,
    lease_token uuid,
    lease_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    finished_at timestamptz
);
CREATE INDEX bp_device_task_items_queue_idx ON bp_device_task_items(id) WHERE status = 'queued';
CREATE INDEX bp_device_task_items_task_idx ON bp_device_task_items(task_id);
CREATE INDEX bp_device_task_items_switch_idx ON bp_device_task_items(switch_id, id DESC);
-- A switch never has two pending operations of one kind.
CREATE UNIQUE INDEX bp_device_task_items_pending_idx ON bp_device_task_items(switch_id, kind)
    WHERE status IN ('queued', 'running');

CREATE TABLE bp_alerts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('device_unreachable', 'backup_failed', 'compliance_violation')),
    severity text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE CASCADE,
    rule_id uuid REFERENCES bp_compliance_rules(id) ON DELETE CASCADE,
    message text NOT NULL,
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'acknowledged', 'resolved')),
    dedupe_key text NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    acknowledged_at timestamptz,
    acknowledged_by text,
    resolved_at timestamptz
);
CREATE UNIQUE INDEX bp_alerts_active_key ON bp_alerts(dedupe_key) WHERE status <> 'resolved';
CREATE INDEX bp_alerts_status_idx ON bp_alerts(status, opened_at DESC);
CREATE INDEX bp_asset_events_action_idx ON bp_asset_events(action, id DESC);
