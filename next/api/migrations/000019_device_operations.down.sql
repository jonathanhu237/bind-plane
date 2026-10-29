DROP INDEX bp_asset_events_action_idx;
DROP TABLE bp_alerts;
DROP TABLE bp_device_task_items;
DROP TABLE bp_device_tasks;
DROP TABLE bp_schedules;
DROP TABLE bp_compliance_results;
DROP TABLE bp_compliance_rules;
DROP TABLE bp_config_backups;
DROP INDEX bp_switches_group_idx;
ALTER TABLE bp_switches
    DROP COLUMN config_error, DROP COLUMN config_checked_at,
    DROP COLUMN reachability_error, DROP COLUMN reachability_latency_ms,
    DROP COLUMN reachability_changed_at, DROP COLUMN reachability_checked_at, DROP COLUMN reachable,
    DROP COLUMN port, DROP COLUMN protocol, DROP COLUMN group_id,
    DROP COLUMN description, DROP COLUMN site, DROP COLUMN model, DROP COLUMN vendor;
DROP TABLE bp_switch_groups;
