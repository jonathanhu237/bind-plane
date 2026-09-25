-- A second worker cannot start another release for the same address while one is active.
CREATE UNIQUE INDEX bp_one_running_release_per_ip ON bp_release_jobs(target_ip)
    WHERE kind='release' AND status='running';
