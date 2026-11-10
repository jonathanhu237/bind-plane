DELETE FROM bp_alerts WHERE kind IN ('snmp_unreachable', 'cpu_high', 'memory_high', 'temperature_high', 'device_restarted');
ALTER TABLE bp_alerts DROP CONSTRAINT bp_alerts_kind_check,
    ADD CONSTRAINT bp_alerts_kind_check CHECK (kind IN ('device_unreachable', 'backup_failed', 'compliance_violation', 'network_read_failed'));

DROP TABLE bp_switch_interfaces;
DROP TABLE bp_switch_metrics;
DROP TABLE bp_switch_monitoring;
DROP TABLE bp_monitoring_settings;
ALTER TABLE bp_command_profiles DROP COLUMN snmp_rules;
ALTER TABLE bp_switches DROP COLUMN snmp_port, DROP COLUMN snmp_credential_id;
DROP TABLE bp_snmp_credentials;
