-- The software version a switch's configuration names, read by the worker from each backup and
-- network read. A status column like the others: writing it never bumps the switch version.
ALTER TABLE bp_switches ADD COLUMN os_version text CHECK (char_length(os_version) BETWEEN 1 AND 100);
