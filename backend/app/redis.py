import redis.asyncio as aioredis

from app.config import settings

redis_client: aioredis.Redis | None = None


async def init_redis() -> None:
    """Initialize the Redis connection."""
    global redis_client
    redis_client = aioredis.Redis(
        host=settings.redis_host,
        port=settings.redis_port,
        password=settings.redis_password,
        db=settings.redis_db,
        decode_responses=True,
    )


async def close_redis() -> None:
    """Close the Redis connection."""
    global redis_client
    if redis_client is not None:
        await redis_client.aclose()
        redis_client = None


def get_redis() -> aioredis.Redis:
    """Return the Redis client. Raises if not initialized."""
    if redis_client is None:
        raise RuntimeError("Redis client is not initialized")
    return redis_client
