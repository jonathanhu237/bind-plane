-- Device monitoring: the worker polls switches over SNMP for their uptime, CPU, memory,
-- temperature and interfaces. Polling only reads. A switch is monitored once it has an SNMP
-- credential, which is stored encrypted like a login credential and never on the switch itself.
CREATE TABLE bp_snmp_credentials (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text UNIQUE NOT NULL,
    snmp_version text NOT NULL CHECK (snmp_version IN ('v2c', 'v3')),
    encrypted_community text,
    username text,
    auth_protocol text CHECK (auth_protocol IN ('md5', 'sha', 'sha224', 'sha256', 'sha384', 'sha512')),
    encrypted_auth_password text,
    priv_protocol text CHECK (priv_protocol IN ('des', 'aes', 'aes192', 'aes256')),
    encrypted_priv_password text,
    description text,
    is_active boolean NOT NULL DEFAULT true,
    version integer NOT NULL DEFAULT 1,
    -- v2c needs only a community; v3 a user, and privacy only on top of authentication.
    CONSTRAINT bp_snmp_credentials_secrets CHECK (
        snmp_version = 'v2c' AND encrypted_community IS NOT NULL AND username IS NULL
            AND auth_protocol IS NULL AND encrypted_auth_password IS NULL AND priv_protocol IS NULL AND encrypted_priv_password IS NULL
        OR snmp_version = 'v3' AND encrypted_community IS NULL AND username IS NOT NULL
            AND (auth_protocol IS NULL) = (encrypted_auth_password IS NULL)
            AND (priv_protocol IS NULL) = (encrypted_priv_password IS NULL)
            AND (priv_protocol IS NULL OR auth_protocol IS NOT NULL))
);

ALTER TABLE bp_switches
    ADD COLUMN snmp_credential_id uuid REFERENCES bp_snmp_credentials(id) ON DELETE RESTRICT,
    ADD COLUMN snmp_port integer CHECK (snmp_port BETWEEN 1 AND 65535);

-- Where a vendor keeps CPU, memory and temperature: each metric lists the OIDs to try in order.
-- Standard MIBs give the rest (system, uptime, interfaces), so an empty object still monitors.
ALTER TABLE bp_command_profiles ADD COLUMN snmp_rules jsonb NOT NULL DEFAULT '{}';
-- The starters get their vendor's OIDs. Nobody can have edited a column that did not exist, so
-- every starter gets them, and its version stays: releases pinned to it keep running.
UPDATE bp_command_profiles p SET snmp_rules = v.rules::jsonb
FROM (VALUES
    ('h3c-comware', $j${"cpu": ["1.3.6.1.4.1.25506.2.6.1.1.1.1.6", "1.3.6.1.4.1.2011.10.2.6.1.1.1.1.6"],
        "memory": ["1.3.6.1.4.1.25506.2.6.1.1.1.1.8", "1.3.6.1.4.1.2011.10.2.6.1.1.1.1.8"],
        "temperature": ["1.3.6.1.4.1.25506.2.6.1.1.1.1.12", "1.3.6.1.4.1.2011.10.2.6.1.1.1.1.12"]}$j$),
    ('huawei-vrp', $j${"cpu": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.5"], "memory": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.7"],
        "temperature": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.11"]}$j$),
    ('huawei-ce', $j${"cpu": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.5"], "memory": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.7"],
        "temperature": ["1.3.6.1.4.1.2011.5.25.31.1.1.1.1.11"]}$j$),
    -- The Huawei-3Com years kept H3C's entity MIB under Huawei's enterprise number.
    ('huawei-quidway', $j${"cpu": ["1.3.6.1.4.1.2011.10.2.6.1.1.1.1.6", "1.3.6.1.4.1.2011.5.25.31.1.1.1.1.5"],
        "memory": ["1.3.6.1.4.1.2011.10.2.6.1.1.1.1.8", "1.3.6.1.4.1.2011.5.25.31.1.1.1.1.7"],
        "temperature": ["1.3.6.1.4.1.2011.10.2.6.1.1.1.1.12", "1.3.6.1.4.1.2011.5.25.31.1.1.1.1.11"]}$j$),
    ('ruijie-rgos', $j${"cpu": ["1.3.6.1.4.1.4881.1.1.10.2.36.1.1.2", "1.3.6.1.4.1.4881.1.1.10.2.36.1.2.1.4"],
        "memory": ["1.3.6.1.4.1.4881.1.1.10.2.35.1.1.1.3", "1.3.6.1.4.1.4881.1.1.10.2.35.1.2.1.3"],
        "temperature": ["1.3.6.1.4.1.4881.1.1.10.2.1.1.23.1.3", "1.3.6.1.4.1.4881.1.1.10.2.1.1.16"]}$j$)
) AS v (seed, rules)
WHERE p.seed = v.seed;

-- One row of thresholds for everyone. A null threshold raises no alert for that metric. It has an
-- id and a name like any asset, so its changes are recorded the same way.
CREATE TABLE bp_monitoring_settings (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL DEFAULT 'monitoring',
    singleton boolean NOT NULL DEFAULT true UNIQUE CHECK (singleton),
    interval_minutes integer NOT NULL DEFAULT 5 CHECK (interval_minutes BETWEEN 1 AND 60),
    cpu_threshold integer CHECK (cpu_threshold BETWEEN 1 AND 100),
    memory_threshold integer CHECK (memory_threshold BETWEEN 1 AND 100),
    temperature_threshold integer CHECK (temperature_threshold BETWEEN 1 AND 150),
    consecutive_polls integer NOT NULL DEFAULT 3 CHECK (consecutive_polls BETWEEN 1 AND 10),
    alert_on_restart boolean NOT NULL DEFAULT true,
    version integer NOT NULL DEFAULT 1
);
INSERT INTO bp_monitoring_settings (cpu_threshold, memory_threshold, temperature_threshold) VALUES (80, 80, 70);

-- The latest poll of each monitored switch. A switch without a row is due at once. `streaks`
-- counts, per metric, the polls in a row above its threshold (positive) or below it (negative).
CREATE TABLE bp_switch_monitoring (
    switch_id uuid PRIMARY KEY REFERENCES bp_switches(id) ON DELETE CASCADE,
    next_poll_at timestamptz NOT NULL DEFAULT now(),
    lease_token uuid,
    lease_until timestamptz,
    polled_at timestamptz,
    succeeded_at timestamptz,
    error text,
    failures integer NOT NULL DEFAULT 0,
    response_ms integer,
    sys_name text,
    sys_descr text,
    sys_object_id text,
    uptime_seconds bigint,
    restarted_at timestamptz,
    cpu_percent smallint,
    memory_percent smallint,
    temperature_c smallint,
    streaks jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX bp_switch_monitoring_due_idx ON bp_switch_monitoring(next_poll_at);

-- One sample per successful poll, kept 30 days.
CREATE TABLE bp_switch_metrics (
    switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE CASCADE,
    sampled_at timestamptz NOT NULL,
    cpu_percent smallint,
    memory_percent smallint,
    temperature_c smallint,
    response_ms integer,
    PRIMARY KEY (switch_id, sampled_at)
);
CREATE INDEX bp_switch_metrics_sampled_idx ON bp_switch_metrics(sampled_at);

-- Each port as last polled. Rates cover the time since the previous poll; the raw counters are
-- kept to work out the next one.
CREATE TABLE bp_switch_interfaces (
    switch_id uuid NOT NULL REFERENCES bp_switches(id) ON DELETE CASCADE,
    if_index integer NOT NULL,
    name text NOT NULL,
    alias text,
    if_type integer,
    admin_up boolean,
    oper_up boolean,
    speed_mbps bigint,
    in_bps bigint,
    out_bps bigint,
    in_errors bigint,
    out_errors bigint,
    new_errors bigint,
    in_octets numeric(20, 0),
    out_octets numeric(20, 0),
    counter_bits smallint NOT NULL DEFAULT 64,
    sampled_at timestamptz NOT NULL,
    oper_changed_at timestamptz,
    PRIMARY KEY (switch_id, if_index)
);

ALTER TABLE bp_alerts DROP CONSTRAINT bp_alerts_kind_check,
    ADD CONSTRAINT bp_alerts_kind_check CHECK (kind IN ('device_unreachable', 'backup_failed', 'compliance_violation', 'network_read_failed',
        'snmp_unreachable', 'cpu_high', 'memory_high', 'temperature_high', 'device_restarted'));
