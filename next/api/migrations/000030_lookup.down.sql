DELETE FROM bp_release_events WHERE job_id IN (SELECT id FROM bp_release_jobs WHERE kind = 'lookup');
DELETE FROM bp_release_jobs WHERE kind = 'lookup';
ALTER TABLE bp_release_jobs DROP CONSTRAINT bp_release_jobs_kind_check,
    ADD CONSTRAINT bp_release_jobs_kind_check CHECK (kind IN ('pre_release_query', 'release'));
