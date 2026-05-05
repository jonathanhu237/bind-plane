import pytest
from bind_plane.core.config import Settings
from pydantic import ValidationError


def test_settings_allow_placeholder_secrets_in_development() -> None:
    settings = Settings(env="development")

    assert settings.debug is True


@pytest.mark.parametrize("field", ["secret_key", "credential_encryption_key"])
def test_settings_reject_placeholder_secrets_outside_development(field: str) -> None:
    values = {
        "env": "production",
        "secret_key": "production-secret",
        "credential_encryption_key": "production-credential-key",
        field: "change-me",
    }

    with pytest.raises(ValidationError):
        Settings(**values)
