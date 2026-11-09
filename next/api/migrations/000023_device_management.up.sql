-- Device management: a connection test that signs in, the time of the last successful
-- backup, and a switch's change history read straight from the asset events.
ALTER TABLE bp_switches
    ADD COLUMN login_ok boolean,
    ADD COLUMN login_checked_at timestamptz,
    ADD COLUMN login_error text,
    ADD COLUMN config_succeeded_at timestamptz;

-- A backup that found no change stored no version but still succeeded, so the last success
-- is the later of the newest version and the newest successful backup run.
UPDATE bp_switches s SET config_succeeded_at = GREATEST(
    (SELECT max(b.captured_at) FROM bp_config_backups b WHERE b.switch_id = s.id),
    (SELECT max(i.finished_at) FROM bp_device_task_items i
     WHERE i.switch_id = s.id AND i.kind = 'config_backup' AND i.status = 'succeeded'));

-- A connection test signs in once and changes nothing; schedules keep the two older kinds.
ALTER TABLE bp_device_tasks DROP CONSTRAINT bp_device_tasks_kind_check,
    ADD CONSTRAINT bp_device_tasks_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check'));
ALTER TABLE bp_device_task_items DROP CONSTRAINT bp_device_task_items_kind_check,
    ADD CONSTRAINT bp_device_task_items_kind_check CHECK (kind IN ('config_backup', 'reachability', 'login_check'));

CREATE INDEX bp_asset_events_object_idx ON bp_asset_events(object_id, id DESC);
