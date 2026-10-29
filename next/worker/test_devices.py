import base64
import json
import os
from unittest.mock import patch

import pytest
from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from worker.devices import (
    BackupRejected,
    config_fingerprint,
    describe_failure,
    normalize_config,
    open_config,
    read_switch_configuration,
    resolve_transport,
    seal_config,
)

# Sealed by the API's sealConfig with an all-zero key.
GO_VECTOR = "v1:JhrwuM15hadmFdoF1_AcW3VvNG0CAYT3S5eIq0HaLlJpj426wdHHMPaEcYm0pgnhmw"


def device(**overrides):
    return {"vendor": "other", "protocol": "telnet", "port": None, **overrides}


def test_transport_keeps_the_legacy_default_and_follows_the_switch_protocol():
    assert resolve_transport(device(), {}) == ("hp_comware_telnet", 23)
    assert resolve_transport(device(vendor="huawei", protocol="ssh"), {}) == ("huawei", 22)
    assert resolve_transport(device(vendor="h3c"), "{}") == ("hp_comware_telnet", 23)
    profile = {"connection_options": {"device_type": "huawei_telnet", "port": 2323}}
    assert resolve_transport(device(vendor="cisco"), profile) == ("huawei_telnet", 2323)
    # A Telnet port written into the profile never leaks into an SSH session.
    assert resolve_transport(device(protocol="ssh"), profile) == ("huawei", 22)
    assert resolve_transport(device(protocol="ssh", port=2222), json.dumps(profile)) == (
        "huawei",
        2222,
    )


def test_configuration_sealing_matches_the_api():
    key = bytes(32)
    assert open_config(GO_VECTOR, key) == "sysname VECTOR\nreturn"
    sealed = seal_config("local-user admin password cipher topsecret", key)
    assert "topsecret" not in sealed
    assert open_config(sealed, key).endswith("topsecret")
    nonce = os.urandom(12)
    credential = AESGCM(key).encrypt(nonce, b"switch-password", None)
    with pytest.raises(InvalidTag):
        open_config("v1:" + base64.urlsafe_b64encode(nonce + credential).rstrip(b"=").decode(), key)


def test_fingerprint_ignores_only_volatile_lines():
    text = normalize_config("\r\n! Last configuration change at 10:00\r\nhostname A  \r\n\r\n")
    assert text == "! Last configuration change at 10:00\nhostname A"
    later = "! Last configuration change at 11:00\nhostname A"
    assert config_fingerprint(text, []) == config_fingerprint(later, [])
    assert config_fingerprint(text, []) != config_fingerprint("hostname B", [])
    assert config_fingerprint("clock 1\nhostname A", [r"^clock"]) == config_fingerprint(
        "clock 2\nhostname A", [r"^clock"]
    )


def profile_dep(templates=None, errors=("^Error:",)):
    return {
        "login_prompt_patterns": "{}",
        "command_templates": json.dumps(
            {"config_backup": "display current-configuration"} if templates is None else templates
        ),
        "prompt_patterns": "{}",
        "pagination_rules": "{}",
        "error_patterns": json.dumps(list(errors)),
        "parser_rules": "{}",
    }


class FakeSession:
    output = ""

    def __init__(self, settings):
        self.settings = settings

    def connect(self):
        pass

    def read_configuration(self, command, disable_paging):
        return self.output

    def close(self):
        pass


@pytest.mark.parametrize(
    "output,reason",
    [
        ("Error: Unrecognized command found\nat '^'\nposition", "error pattern"),
        ("sysname X\n\n", "too short"),
    ],
)
def test_backup_output_that_is_not_a_configuration_is_rejected(output, reason):
    FakeSession.output = output
    with (
        patch("worker.devices.ConfigurationSession", FakeSession),
        pytest.raises(BackupRejected, match=reason),
    ):
        read_switch_configuration(profile_dep(), lambda dep, profile: None)


def test_backup_requires_a_backup_command_and_normalizes_output():
    with pytest.raises(BackupRejected, match="config_backup"):
        read_switch_configuration(profile_dep(templates={}), lambda dep, profile: None)
    FakeSession.output = "\r\nsysname X\r\n#\r\nreturn   \r\n"
    with patch("worker.devices.ConfigurationSession", FakeSession):
        assert (
            read_switch_configuration(profile_dep(), lambda dep, profile: None)
            == "sysname X\n#\nreturn"
        )


def test_failure_messages_never_echo_device_output():
    assert (
        describe_failure(ConnectionRefusedError("10.0.0.1:23 refused, password=hunter2"))
        == "Connection refused"
    )
    assert (
        describe_failure(BackupRejected("Backup output is too short"))
        == "Backup output is too short"
    )
    assert "hunter2" not in describe_failure(ValueError("hunter2"))
