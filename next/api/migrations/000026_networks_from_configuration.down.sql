DELETE FROM bp_alerts WHERE kind = 'network_read_failed';
ALTER TABLE bp_alerts DROP CONSTRAINT bp_alerts_kind_check,
    ADD CONSTRAINT bp_alerts_kind_check CHECK (kind IN ('device_unreachable', 'backup_failed', 'compliance_violation'));
DELETE FROM bp_device_task_items WHERE kind = 'network_read';
DELETE FROM bp_device_tasks WHERE kind = 'network_read';
ALTER TABLE bp_device_task_items DROP CONSTRAINT bp_device_task_items_kind_check,
    ADD CONSTRAINT bp_device_task_items_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check'));
ALTER TABLE bp_device_tasks DROP CONSTRAINT bp_device_tasks_kind_check,
    ADD CONSTRAINT bp_device_tasks_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check'));

-- Networks read from configuration stay, as networks someone validated; the unused ones the
-- migration up removed do not come back.
ALTER TABLE bp_networks DROP COLUMN interface;
ALTER TABLE bp_switches
    DROP COLUMN networks_error,
    DROP COLUMN networks_succeeded_at,
    DROP COLUMN networks_checked_at,
    DROP COLUMN handles_release;
