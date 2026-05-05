from uuid import UUID

from redis import Redis
from rq import Queue
from rq.exceptions import DuplicateJobError

from bind_plane.core.config import Settings, get_settings

RELEASE_QUEUE_NAME = "release"
RELEASE_JOB_TIMEOUT_SECONDS = 15 * 60


def get_release_queue(settings: Settings | None = None) -> Queue:
    resolved_settings = settings or get_settings()
    return Queue(
        RELEASE_QUEUE_NAME,
        connection=Redis.from_url(resolved_settings.redis_url),
        default_timeout=RELEASE_JOB_TIMEOUT_SECONDS,
    )


def release_rq_job_id(job_id: UUID) -> str:
    return f"release:{job_id}"


def pre_release_query_rq_job_id(job_id: UUID) -> str:
    return f"pre-release-query:{job_id}"


def _enqueue_once(
    *,
    function_path: str,
    rq_job_id: str,
    release_job_id: UUID,
) -> None:
    try:
        get_release_queue().enqueue_call(
            function_path,
            args=(str(release_job_id),),
            timeout=RELEASE_JOB_TIMEOUT_SECONDS,
            job_id=rq_job_id,
            unique=True,
        )
    except DuplicateJobError:
        return


def enqueue_release_job(job_id: UUID) -> None:
    _enqueue_once(
        function_path="bind_plane.worker.main.run_release_job",
        rq_job_id=release_rq_job_id(job_id),
        release_job_id=job_id,
    )


def enqueue_pre_release_query_job(job_id: UUID) -> None:
    _enqueue_once(
        function_path="bind_plane.worker.main.run_pre_release_query_job",
        rq_job_id=pre_release_query_rq_job_id(job_id),
        release_job_id=job_id,
    )
