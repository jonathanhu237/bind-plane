ALTER TABLE auth_roles DISABLE TRIGGER bp_protect_roles_update;
UPDATE auth_roles SET description = 'Can administer bind-plane and force a release.'
WHERE name_canonical = 'admin' AND description = 'Can release bindings and administer bind-plane.';
ALTER TABLE auth_roles ENABLE TRIGGER bp_protect_roles_update;
