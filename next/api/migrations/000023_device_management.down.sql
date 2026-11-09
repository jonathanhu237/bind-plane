DROP INDEX bp_asset_events_object_idx;

DELETE FROM bp_device_task_items WHERE kind = 'login_check';
DELETE FROM bp_device_tasks WHERE kind = 'login_check';
ALTER TABLE bp_device_task_items DROP CONSTRAINT bp_device_task_items_kind_check,
    ADD CONSTRAINT bp_device_task_items_kind_check CHECK (kind IN ('config_backup', 'reachability'));
ALTER TABLE bp_device_tasks DROP CONSTRAINT bp_device_tasks_kind_check,
    ADD CONSTRAINT bp_device_tasks_kind_check CHECK (kind IN ('config_backup', 'reachability'));

ALTER TABLE bp_switches
    DROP COLUMN config_succeeded_at,
    DROP COLUMN login_error,
    DROP COLUMN login_checked_at,
    DROP COLUMN login_ok;
