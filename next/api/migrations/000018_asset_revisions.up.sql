-- Asset edits are versioned. Each job pins the switch/profile revision its pre-query observed,
-- so an edited asset can never silently redirect a confirmed release to different hardware.
ALTER TABLE bp_credentials ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE bp_command_profiles ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE bp_switches ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE bp_networks ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE bp_asset_events ADD COLUMN changes jsonb NOT NULL DEFAULT '{}';

ALTER TABLE bp_release_jobs
    ADD COLUMN switch_version integer, ADD COLUMN command_profile_version integer,
    ADD COLUMN switch_name text, ADD COLUMN switch_management_ip inet,
    ADD COLUMN resolved_network_id uuid;
-- Every asset starts at version 1 above, so existing jobs match the only revision there is.
UPDATE bp_release_jobs j SET switch_version=1, command_profile_version=1,
    switch_name=s.name, switch_management_ip=s.management_ip,
    resolved_network_id=CASE WHEN j.result->>'network_id' ~ '^[0-9a-f-]{36}$'
        THEN (j.result->>'network_id')::uuid END
FROM bp_switches s WHERE s.id=j.switch_id;
-- No defaults: a job inserted without a pinned revision must fail rather than run unfenced.
ALTER TABLE bp_release_jobs
    ALTER COLUMN switch_version SET NOT NULL, ALTER COLUMN command_profile_version SET NOT NULL,
    ALTER COLUMN switch_name SET NOT NULL, ALTER COLUMN switch_management_ip SET NOT NULL;
