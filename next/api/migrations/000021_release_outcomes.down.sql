UPDATE bp_release_events SET status = 'cancelled' WHERE status IN ('no_static_binding', 'no_record');
UPDATE bp_release_jobs SET status = 'cancelled' WHERE status IN ('no_static_binding', 'no_record');
