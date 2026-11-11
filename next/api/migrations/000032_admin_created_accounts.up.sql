-- An administrator can create an account with an initial password. When they ask for it, the
-- account must replace that password at its first sign-in before it can do anything else.
ALTER TABLE auth_users ADD COLUMN password_change_required boolean NOT NULL DEFAULT false;
