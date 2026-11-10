-- Zabbix watches whether switches are up, so Bind Plane stops checking their ports on its own:
-- port-check schedules and runs, the switch-unreachable alert and the reachability columns go.
-- What stays is the connection test, which tells whether Bind Plane can sign in to a switch.

-- A port that last failed after the switch last signed in is, as far as Bind Plane knows, a
-- switch it cannot sign in to; keep that as the connection result before the columns go.
UPDATE bp_switches
SET login_ok = false, login_error = reachability_error, login_checked_at = reachability_checked_at
WHERE reachable = false AND reachability_error IS NOT NULL
  AND (login_checked_at IS NULL OR login_checked_at < reachability_checked_at);

DELETE FROM bp_schedules WHERE kind = 'reachability';
DELETE FROM bp_device_tasks WHERE kind = 'reachability';
DELETE FROM bp_alerts WHERE kind = 'device_unreachable';

ALTER TABLE bp_schedules DROP CONSTRAINT bp_schedules_kind_check,
    ADD CONSTRAINT bp_schedules_kind_check CHECK (kind IN ('config_backup'));
ALTER TABLE bp_device_tasks DROP CONSTRAINT bp_device_tasks_kind_check,
    ADD CONSTRAINT bp_device_tasks_kind_check CHECK (kind IN ('config_backup', 'login_check', 'network_read'));
ALTER TABLE bp_device_task_items DROP CONSTRAINT bp_device_task_items_kind_check,
    ADD CONSTRAINT bp_device_task_items_kind_check CHECK (kind IN ('config_backup', 'login_check', 'network_read'));
ALTER TABLE bp_alerts DROP CONSTRAINT bp_alerts_kind_check,
    ADD CONSTRAINT bp_alerts_kind_check CHECK (kind IN ('backup_failed', 'compliance_violation', 'network_read_failed'));

ALTER TABLE bp_switches
    DROP COLUMN reachable,
    DROP COLUMN reachability_checked_at,
    DROP COLUMN reachability_changed_at,
    DROP COLUMN reachability_latency_ms,
    DROP COLUMN reachability_error;
