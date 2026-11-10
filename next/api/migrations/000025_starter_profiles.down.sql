-- Starters that switches or release jobs still use stay behind as ordinary profiles.
DELETE FROM bp_command_profiles p
WHERE p.seed IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM bp_switches s WHERE s.command_profile_id = p.id)
  AND NOT EXISTS (SELECT 1 FROM bp_release_jobs j WHERE j.command_profile_id = p.id);
ALTER TABLE bp_command_profiles DROP COLUMN seed;
