import base64
import hashlib
import hmac
import json
import time

from bind_plane.core.config import get_settings


class TokenError(Exception):
    pass


def _b64encode(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode("ascii")


def _b64decode(data: str) -> bytes:
    padding = "=" * (-len(data) % 4)
    return base64.urlsafe_b64decode(data + padding)


def _sign(payload: str) -> str:
    secret = get_settings().secret_key.encode("utf-8")
    return _b64encode(hmac.new(secret, payload.encode("ascii"), hashlib.sha256).digest())


def create_access_token(subject: str, ttl_seconds: int = 8 * 60 * 60) -> str:
    payload = _b64encode(
        json.dumps(
            {"sub": subject, "exp": int(time.time()) + ttl_seconds},
            separators=(",", ":"),
        ).encode("utf-8")
    )
    return f"{payload}.{_sign(payload)}"


def verify_access_token(token: str) -> str:
    try:
        payload, signature = token.split(".", 1)
    except ValueError as exc:
        raise TokenError("Malformed token") from exc

    if not hmac.compare_digest(signature, _sign(payload)):
        raise TokenError("Invalid token signature")

    data = json.loads(_b64decode(payload))
    if int(data["exp"]) < int(time.time()):
        raise TokenError("Expired token")
    return str(data["sub"])
