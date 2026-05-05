import base64
import hashlib

from cryptography.fernet import Fernet

from bind_plane.core.config import get_settings


def _fernet() -> Fernet:
    raw_key = get_settings().credential_encryption_key.encode("utf-8")
    key = base64.urlsafe_b64encode(hashlib.sha256(raw_key).digest())
    return Fernet(key)


def encrypt_secret(value: str) -> str:
    return _fernet().encrypt(value.encode("utf-8")).decode("ascii")


def decrypt_secret(value: str) -> str:
    return _fernet().decrypt(value.encode("ascii")).decode("utf-8")
