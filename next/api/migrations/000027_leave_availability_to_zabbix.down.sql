-- Brings the port checks' columns and kinds back, empty: the deleted schedules, runs and
-- alerts are not restored.
ALTER TABLE bp_switches
    ADD COLUMN reachable boolean,
    ADD COLUMN reachability_checked_at timestamptz,
    ADD COLUMN reachability_changed_at timestamptz,
    ADD COLUMN reachability_latency_ms integer,
    ADD COLUMN reachability_error text;

ALTER TABLE bp_alerts DROP CONSTRAINT bp_alerts_kind_check,
    ADD CONSTRAINT bp_alerts_kind_check CHECK (kind IN ('device_unreachable', 'backup_failed', 'compliance_violation', 'network_read_failed'));
ALTER TABLE bp_device_task_items DROP CONSTRAINT bp_device_task_items_kind_check,
    ADD CONSTRAINT bp_device_task_items_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check', 'network_read'));
ALTER TABLE bp_device_tasks DROP CONSTRAINT bp_device_tasks_kind_check,
    ADD CONSTRAINT bp_device_tasks_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check', 'network_read'));
ALTER TABLE bp_schedules DROP CONSTRAINT bp_schedules_kind_check,
    ADD CONSTRAINT bp_schedules_kind_check CHECK (kind IN ('config_backup', 'reachability'));
