import secrets
from dataclasses import dataclass

import redis.asyncio as aioredis

from app.config import settings

SESSION_KEY_PREFIX = "session:"


@dataclass
class SessionResult:
    session_id: str
    expires_in: int  # seconds


class SessionStore:
    """Manages sessions in Redis, following the same pattern as rota's session/store.go."""

    def __init__(self, redis: aioredis.Redis) -> None:
        self._redis = redis
        self._ttl = settings.session_expires_hours * 3600

    async def create(self, user_id: str) -> SessionResult:
        """Create a new session for the given user ID."""
        session_id = secrets.token_hex(32)
        key = f"{SESSION_KEY_PREFIX}{session_id}"
        await self._redis.set(key, user_id, ex=self._ttl)
        return SessionResult(session_id=session_id, expires_in=self._ttl)

    async def get(self, session_id: str) -> str | None:
        """Get the user ID for a session. Returns None if not found or expired."""
        key = f"{SESSION_KEY_PREFIX}{session_id}"
        return await self._redis.get(key)

    async def refresh(self, session_id: str) -> int:
        """Refresh the session TTL. Returns the new TTL in seconds."""
        key = f"{SESSION_KEY_PREFIX}{session_id}"
        await self._redis.expire(key, self._ttl)
        return self._ttl

    async def delete(self, session_id: str) -> None:
        """Delete a session."""
        key = f"{SESSION_KEY_PREFIX}{session_id}"
        await self._redis.delete(key)
