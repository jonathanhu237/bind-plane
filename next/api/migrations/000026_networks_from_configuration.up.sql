-- Networks come from the switches that handle releases: each is an interface address in the
-- switch's running configuration, read by the worker. Nobody types one in, so nothing waits to be
-- validated by hand, and a mistyped network cannot send a release to the wrong switch.
ALTER TABLE bp_switches
    ADD COLUMN handles_release boolean NOT NULL DEFAULT false,
    ADD COLUMN networks_checked_at timestamptz,
    ADD COLUMN networks_succeeded_at timestamptz,
    ADD COLUMN networks_error text;
-- The interface a network was read from; null for one typed in before this migration.
ALTER TABLE bp_networks ADD COLUMN interface text;

-- A switch that releases today keeps doing so. Its typed-in networks stay until the first read
-- of its configuration replaces them; with networks_checked_at null, that read is queued at once.
UPDATE bp_switches s SET handles_release = true
WHERE EXISTS (SELECT 1 FROM bp_networks n WHERE n.switch_id = s.id AND n.is_enabled AND n.is_validated);
-- The rest were never used by a release (disabled or never validated), and nothing reads them now.
DELETE FROM bp_networks n USING bp_switches s WHERE s.id = n.switch_id AND NOT s.handles_release;

ALTER TABLE bp_device_tasks DROP CONSTRAINT bp_device_tasks_kind_check,
    ADD CONSTRAINT bp_device_tasks_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check', 'network_read'));
ALTER TABLE bp_device_task_items DROP CONSTRAINT bp_device_task_items_kind_check,
    ADD CONSTRAINT bp_device_task_items_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check', 'network_read'));
ALTER TABLE bp_alerts DROP CONSTRAINT bp_alerts_kind_check,
    ADD CONSTRAINT bp_alerts_kind_check CHECK (kind IN ('device_unreachable', 'backup_failed', 'compliance_violation', 'network_read_failed'));
