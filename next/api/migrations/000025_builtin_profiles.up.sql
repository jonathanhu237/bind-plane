-- Command profiles that ship with Bind Plane. Their commands are the ones the campus's previous
-- release system used for years; their parsing rules were written against replies captured
-- read-only from live switches (next/worker/testdata/arp_samples.json). Later migrations keep
-- them up to date, so the API leaves their content as shipped: copy one to change it.
ALTER TABLE bp_command_profiles ADD COLUMN builtin text UNIQUE;

INSERT INTO bp_command_profiles (builtin, name, vendor, command_templates, parser_rules, pagination_rules, error_patterns)
SELECT v.builtin,
       -- A profile someone already named like this keeps its name.
       v.name || CASE WHEN EXISTS (SELECT 1 FROM bp_command_profiles p WHERE p.name = v.name) THEN '（内置）' ELSE '' END,
       v.vendor, v.command_templates::jsonb, v.parser_rules::jsonb, v.pagination_rules::jsonb, v.error_patterns::jsonb
FROM (VALUES
    ('h3c-comware', 'H3C Comware', 'h3c',
     $j${"single_arp_query": "display arp $ip", "arp_release": "undo arp $ip", "super": "super",
         "config": "system-view", "exit_config": "return", "config_backup": "display current-configuration"}$j$,
     $j${"arp_entry_regex": "^(?P<ip>\\d{1,3}(?:\\.\\d{1,3}){3})\\s+(?P<mac>[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4})\\s+\\S+\\s+\\S+\\s+\\S+\\s+(?P<type>[A-Z])\\s*$",
         "static_type_values": ["S"], "dynamic_type_values": ["D"], "missing_patterns": ["IP Address\\s+MAC Address"]}$j$,
     $j${"disable_paging_command": "screen-length disable"}$j$,
     $j$["% Unrecognized command", "% Incomplete command", "% Wrong parameter", "% Too many parameters", "Error:"]$j$),
    ('huawei-vrp', '华为 VRP（S 系列）', 'huawei',
     $j${"single_arp_query": "display arp | include $ip", "arp_release": "undo arp static $ip", "super": "super",
         "config": "system-view", "exit_config": "return", "config_backup": "display current-configuration"}$j$,
     $j${"arp_entry_regex": "^(?P<ip>\\d{1,3}(?:\\.\\d{1,3}){3})\\s+(?P<mac>[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4})\\s+(?:\\d+\\s+)?(?P<type>[A-Z])(?=[-/\\s]|$)",
         "static_type_values": ["S"], "dynamic_type_values": ["D"], "missing_patterns": ["IP ADDRESS\\s+MAC ADDRESS"]}$j$,
     $j${"disable_paging_command": "screen-length 0 temporary"}$j$,
     $j$["Error:"]$j$),
    ('huawei-ce', '华为 CE 系列', 'huawei',
     $j${"single_arp_query": "display arp | include $ip", "arp_release": "undo arp static $ip",
         "config": "system-view immediately", "exit_config": "return", "config_backup": "display current-configuration"}$j$,
     $j${"arp_entry_regex": "^(?P<ip>\\d{1,3}(?:\\.\\d{1,3}){3})\\s+(?P<mac>[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4})\\s+(?:\\d+\\s+)?(?P<type>[A-Z])(?=[-/\\s]|$)",
         "static_type_values": ["S"], "dynamic_type_values": ["D"], "missing_patterns": ["IP ADDRESS\\s+MAC ADDRESS"]}$j$,
     $j${"disable_paging_command": "screen-length 0 temporary"}$j$,
     $j$["Error:"]$j$),
    -- Quidway E-series, S3050C, S5516 and S5624P came from the Huawei-3Com years and use H3C's
    -- commands. No live sample was available, so their replies are assumed to read like H3C's.
    ('huawei-quidway', '华为 Quidway 老款', 'huawei',
     $j${"single_arp_query": "display arp $ip", "arp_release": "undo arp $ip", "super": "super",
         "config": "system-view", "exit_config": "return", "config_backup": "display current-configuration"}$j$,
     $j${"arp_entry_regex": "^(?P<ip>\\d{1,3}(?:\\.\\d{1,3}){3})\\s+(?P<mac>[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4})\\s+\\S+\\s+\\S+\\s+\\S+\\s+(?P<type>[A-Z])\\s*$",
         "static_type_values": ["S"], "dynamic_type_values": ["D"], "missing_patterns": ["IP Address\\s+MAC Address"]}$j$,
     $j${}$j$,
     $j$["% Unrecognized command", "% Incomplete command", "% Wrong parameter", "% Too many parameters", "Error:"]$j$),
    -- A Ruijie row's age says whether it is static: <static>, or minutes left for a dynamic entry.
    ('ruijie-rgos', '锐捷 RGOS', 'ruijie',
     $j${"single_arp_query": "show arp $ip", "arp_release": "no arp $ip", "super": "enable",
         "config": "configure terminal", "exit_config": "end", "config_backup": "show running-config"}$j$,
     $j${"arp_entry_regex": "^Internet\\s+(?P<ip>\\d{1,3}(?:\\.\\d{1,3}){3})\\s+(?P<type><static>|--|\\d+)\\s+(?P<mac>[0-9a-f]{4}\\.[0-9a-f]{4}\\.[0-9a-f]{4})\\s",
         "static_type_values": ["<static>"], "dynamic_type_pattern": "\\d+",
         "missing_patterns": ["No such ARP entries exist", "Protocol\\s+Address\\s+Age"]}$j$,
     $j${"disable_paging_command": "terminal length 0"}$j$,
     $j$["% Invalid input", "% Unknown command", "% Incomplete command", "% Ambiguous command"]$j$)
) AS v (builtin, name, vendor, command_templates, parser_rules, pagination_rules, error_patterns);
