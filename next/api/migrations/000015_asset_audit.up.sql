-- Durable asset changes, separate from Temvia's best-effort/retained operation history.
CREATE TABLE bp_asset_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_id uuid REFERENCES auth_users(id) ON DELETE SET NULL,
    actor_email text NOT NULL,
    action text NOT NULL,
    object_id uuid NOT NULL,
    object_name text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX bp_asset_events_created_idx ON bp_asset_events(created_at DESC);
