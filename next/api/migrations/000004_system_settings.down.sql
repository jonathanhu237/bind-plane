DELETE FROM auth_role_permissions AS legacy
USING auth_role_permissions AS current
WHERE legacy.role_id = current.role_id
  AND legacy.permission_key = 'invitations.write'
  AND current.permission_key = 'invitations.manage';

UPDATE auth_role_permissions
SET permission_key = 'invitations.manage'
WHERE permission_key = 'invitations.write';

DROP TABLE IF EXISTS auth_email_settings;
