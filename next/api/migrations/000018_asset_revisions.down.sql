ALTER TABLE bp_release_jobs DROP COLUMN resolved_network_id, DROP COLUMN switch_management_ip,
    DROP COLUMN switch_name, DROP COLUMN command_profile_version, DROP COLUMN switch_version;
ALTER TABLE bp_asset_events DROP COLUMN changes;
ALTER TABLE bp_networks DROP COLUMN version;
ALTER TABLE bp_switches DROP COLUMN version;
ALTER TABLE bp_command_profiles DROP COLUMN version;
ALTER TABLE bp_credentials DROP COLUMN version;
