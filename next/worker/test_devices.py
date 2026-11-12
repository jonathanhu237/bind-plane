import base64
import json
import os
import re
from types import SimpleNamespace
from unittest.mock import patch

import pytest
from cryptography.exceptions import InvalidTag
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from worker.devices import (
    BackupRejected,
    ConfigurationSession,
    config_fingerprint,
    describe_failure,
    normalize_config,
    open_config,
    os_version,
    read_paged,
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
    elevated = None

    def __init__(self, settings):
        self.settings = settings

    def connect(self):
        pass

    def read_configuration(self, command, disable_paging, elevate_privilege=None):
        FakeSession.elevated = elevate_privilege
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


def test_configuration_is_read_at_the_privileged_level():
    # The profile's super (or enable) command goes to the session; without one, nothing is sent.
    FakeSession.output = "sysname X\n#\nreturn"
    templates = {"config_backup": "display current-configuration", "super": "super"}
    with patch("worker.devices.ConfigurationSession", FakeSession):
        read_switch_configuration(profile_dep(templates=templates), lambda dep, profile: None)
        assert FakeSession.elevated == "super"
        read_switch_configuration(
            profile_dep(templates={"config_backup": "display current-configuration"}),
            lambda dep, profile: None,
        )
        assert FakeSession.elevated is None


class PagedSwitch:
    """A switch that answers super with a password prompt and shows its configuration only at
    the privileged level, page by page, wiping each More prompt the way it really does."""

    base_prompt = "X"

    def __init__(self, pages, *, wants_super=True):
        self.pages = list(pages)
        self.privileged = not wants_super
        self.calls = []

    def normalize_cmd(self, command):
        return command + "\n"

    def disable_paging(self, *, command):
        self.calls.append(command)

    def send_command(self, command, expect_string=None, **kwargs):
        self.calls.append(command)
        if command == "super":
            return "super\nPassword:"
        if command == "super-secret":
            self.privileged = True
            return "User privilege level is 3\n<X>"
        return ""

    def write_channel(self, data):
        self.calls.append(data)

    def read_until_pattern(self, *, pattern, read_timeout):
        if not self.privileged:
            return "display current-configuration\r\n  % Unrecognized command\r\n<X>"
        page = self.pages.pop(0)
        assert re.search(pattern, page)
        return page


H3C_WIPE = "\x1b[42D" + " " * 42 + "\x1b[42D"
RUIJIE_WIPE = "\x08" * 10 + " " * 10 + "\x08" * 10


def test_configuration_session_enters_super_before_reading():
    switch = PagedSwitch(["display current-configuration\r\n#\r\n sysname X\r\n#\r\nreturn\r\n<X>"])
    session = ConfigurationSession(SimpleNamespace(secret="super-secret", timeout=20))
    session._connection = switch
    text = session.read_configuration(
        "display current-configuration", "screen-length disable", "super"
    )
    assert normalize_config(text) == "#\n sysname X\n#\nreturn"
    assert switch.calls[:3] == ["super", "super-secret", "screen-length disable"]


@pytest.mark.parametrize("more,wipe", [("  ---- More ----", H3C_WIPE), (" --More-- ", RUIJIE_WIPE)])
def test_paged_configuration_is_read_to_the_end(more, wipe):
    # Old switches cannot turn paging off: each More prompt is answered with a space and
    # disappears from the configuration, and indentation after a page break is kept.
    switch = PagedSwitch(
        [
            "display current-configuration\r\n#\r\n sysname X\r\n#\r\n"
            "interface Vlan-interface10\r\n" + more,
            wipe + " ip address 10.1.1.1 255.255.255.0\r\n#\r\n" + more,
            wipe + "return\r\n<X>",
        ],
        wants_super=False,
    )
    text = read_paged(switch, "display current-configuration")
    assert normalize_config(text) == (
        "#\n sysname X\n#\ninterface Vlan-interface10\n"
        " ip address 10.1.1.1 255.255.255.0\n#\nreturn"
    )
    assert switch.calls == ["display current-configuration\n", " ", " "]


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


@pytest.mark.parametrize(
    ("config", "version"),
    [
        ("!Software Version V200R019C10SPC500\n#\nsysname CORE\n", "V200R019C10SPC500"),
        ("#\n version 7.1.070, Release 6616P05\n#\n sysname H3C\n", "7.1.070, Release 6616P05"),
        (
            "!\nversion RGOS 10.4(3b23)p2 Release(232237)\n!\nhostname RJ\n",
            "RGOS 10.4(3b23)p2 Release(232237)",
        ),
        ("version 15.2\nhostname cisco\n", "15.2"),
        ("sysname NONE\n#\nsnmp-agent sys-info version v2c\n", None),
        ("#\n" * 50 + "version 1.0\n", None),  # only the top of a configuration names its software
    ],
)
def test_os_version_reads_the_software_a_configuration_names(config, version):
    assert os_version(config) == version


def test_a_command_profile_can_say_where_its_version_is():
    rules = {"os_version_patterns": ["(", r"^Firmware: (\S+)"]}
    assert os_version("Firmware: 2.4.1\nversion 9.9\n", rules) == "2.4.1"
    assert os_version("version 9.9\n", json.dumps(rules)) is None
