-- Records show who acted by name. The name is kept as it was when they acted, like the email
-- beside it, so renaming or deleting the account later leaves the record unchanged.
ALTER TABLE bp_release_jobs ADD COLUMN actor_name text;
ALTER TABLE bp_asset_events ADD COLUMN actor_name text;
ALTER TABLE bp_device_tasks ADD COLUMN actor_name text;

UPDATE bp_release_jobs j SET actor_name = u.name FROM auth_users u WHERE u.id = j.actor_id;
UPDATE bp_asset_events e SET actor_name = u.name FROM auth_users u WHERE u.id = e.actor_id;
UPDATE bp_device_tasks t SET actor_name = u.name FROM auth_users u WHERE u.id = t.actor_id;
