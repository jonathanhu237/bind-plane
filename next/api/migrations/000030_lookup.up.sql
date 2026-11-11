-- A lookup asks the switch for one address's ARP entry (its MAC, and whether it is a static
-- binding) and changes nothing. It is a job like a release, so who looked up what stays on record,
-- but it takes no address lock: a lookup never holds up a release.
ALTER TABLE bp_release_jobs DROP CONSTRAINT bp_release_jobs_kind_check,
    ADD CONSTRAINT bp_release_jobs_kind_check CHECK (kind IN ('pre_release_query', 'release', 'lookup'));
