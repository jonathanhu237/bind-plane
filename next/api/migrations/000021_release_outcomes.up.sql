-- A release that sent nothing says why instead of a bare "cancelled": the address had only a
-- dynamic entry, or the switch had no record of it. The query before release recorded which.
UPDATE bp_release_events e
SET status = CASE j.before_state->>'entry_type' WHEN 'dynamic' THEN 'no_static_binding' ELSE 'no_record' END
FROM bp_release_jobs j
WHERE e.job_id = j.id AND e.status = 'cancelled' AND j.status = 'cancelled'
    AND j.before_state->>'entry_type' IN ('dynamic', 'missing');

UPDATE bp_release_jobs
SET status = CASE before_state->>'entry_type' WHEN 'dynamic' THEN 'no_static_binding' ELSE 'no_record' END
WHERE status = 'cancelled' AND before_state->>'entry_type' IN ('dynamic', 'missing');
