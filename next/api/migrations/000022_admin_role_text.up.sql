-- Forced release is gone; the seeded Admin description no longer promises it. An edited one is kept.
-- The business roles are otherwise unchangeable, so the guard steps aside for this update only.
ALTER TABLE auth_roles DISABLE TRIGGER bp_protect_roles_update;
UPDATE auth_roles SET description = 'Can release bindings and administer bind-plane.'
WHERE name_canonical = 'admin' AND description = 'Can administer bind-plane and force a release.';
ALTER TABLE auth_roles ENABLE TRIGGER bp_protect_roles_update;
