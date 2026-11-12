-- The newest version of the guided tour each user has finished or skipped. The tour shows at a
-- user's first sign-in, whichever browser it is in, and again only when a newer version is published.
CREATE TABLE bp_user_guides (
    user_id uuid PRIMARY KEY REFERENCES auth_users(id) ON DELETE CASCADE,
    version integer NOT NULL CHECK (version > 0),
    seen_at timestamptz NOT NULL DEFAULT now()
);
