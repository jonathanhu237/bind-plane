DROP TABLE bp_release_events;
DROP TABLE bp_release_jobs;
DROP TABLE bp_networks;
DROP TABLE bp_switches;
DROP TABLE bp_command_profiles;
DROP TABLE bp_credentials;
DROP TRIGGER bp_protect_roles_update ON auth_roles;
DROP FUNCTION bp_protect_roles();
DELETE FROM auth_roles WHERE name_canonical IN ('operator', 'admin');
