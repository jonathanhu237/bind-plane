ALTER TABLE bp_switches DROP CONSTRAINT bp_switches_profile_vendor_fkey;
ALTER TABLE bp_command_profiles DROP CONSTRAINT bp_command_profiles_id_vendor_key;
ALTER TABLE bp_command_profiles DROP COLUMN vendor;
