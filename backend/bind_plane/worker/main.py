import asyncio
from uuid import UUID

from redis import Redis
from rq import Worker
from sqlalchemy import select

from bind_plane.core.config import get_settings
from bind_plane.db.models import ReleaseJob, ReleaseJobKind, ReleaseJobStatus
from bind_plane.db.session import async_session
from bind_plane.worker.queue import (
    RELEASE_QUEUE_NAME,
    enqueue_pre_release_query_job,
    enqueue_release_job,
)
from bind_plane.worker.release_executor import execute_pre_release_query_job, execute_release_job


async def _run_release_job(job_id: UUID) -> None:
    async with async_session() as session:
        await execute_release_job(session, job_id)


async def _run_pre_release_query_job(job_id: UUID) -> None:
    async with async_session() as session:
        await execute_pre_release_query_job(session, job_id)


def run_release_job(job_id: str) -> str:
    asyncio.run(_run_release_job(UUID(job_id)))
    return job_id


def run_pre_release_query_job(job_id: str) -> str:
    asyncio.run(_run_pre_release_query_job(UUID(job_id)))
    return job_id


async def _reconcile_queued_jobs() -> None:
    async with async_session() as session:
        result = await session.execute(
            select(ReleaseJob.id, ReleaseJob.kind).where(
                ReleaseJob.status == ReleaseJobStatus.QUEUED
            )
        )
        for job_id, kind in result.all():
            if kind == ReleaseJobKind.PRE_RELEASE_QUERY:
                enqueue_pre_release_query_job(job_id)
            elif kind == ReleaseJobKind.RELEASE:
                enqueue_release_job(job_id)


def main() -> None:
    settings = get_settings()
    connection = Redis.from_url(settings.redis_url)
    asyncio.run(_reconcile_queued_jobs())
    worker = Worker([RELEASE_QUEUE_NAME], connection=connection)
    worker.work()


if __name__ == "__main__":
    main()
