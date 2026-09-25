CREATE TABLE auth_system_identity (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton = true),
    system_name text NOT NULL DEFAULT 'Temvia',
    icon_media_type text NOT NULL DEFAULT '',
    icon_bytes bytea,
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT auth_system_identity_name_length CHECK (char_length(system_name) BETWEEN 1 AND 50),
    CONSTRAINT auth_system_identity_icon_type CHECK (icon_media_type IN ('', 'image/png', 'image/jpeg', 'image/webp')),
    CONSTRAINT auth_system_identity_icon_pair CHECK ((icon_media_type = '') = (icon_bytes IS NULL)),
    CONSTRAINT auth_system_identity_icon_size CHECK (icon_bytes IS NULL OR octet_length(icon_bytes) BETWEEN 1 AND 2097152),
    CONSTRAINT auth_system_identity_revision_positive CHECK (revision > 0)
);

INSERT INTO auth_system_identity (singleton, system_name, icon_media_type, icon_bytes, revision)
VALUES (true, 'Temvia', '', NULL, 1);
