from types import SimpleNamespace
from unittest.mock import patch

from worker.main import device_task


def dependency():
    return {
        "is_enabled": True,
        "credential_active": True,
        "profile_active": True,
        "management_ip": "192.0.2.1",
        "username": "test",
        "encrypted_password": "unused",
        "encrypted_secret": None,
        "login_prompt_patterns": {},
        "command_templates": {"single_arp_query": "show arp $ip", "arp_release": "undo arp $ip"},
        "prompt_patterns": {},
        "pagination_rules": {},
        "success_patterns": [],
        "error_patterns": [],
        "parser_rules": {
            "arp_entry_regex": r"(?P<ip>\d+\.\d+\.\d+\.\d+) (?P<mac>[0-9a-f:]+) (?P<type>\w+)",
            "static_type_values": ["static"],
            "dynamic_type_values": ["dynamic"],
            "missing_patterns": ["not found"],
        },
    }


class Device:
    def __init__(self, settings):
        self.release_calls = 0

    def connect(self):
        pass

    def query_before(self, commands):
        return "10.1.2.3 aa:bb static"

    def release(self, commands):
        self.release_calls += 1
        return "OK"

    def query_after(self, commands):
        return "10.1.2.3 aa:bb dynamic"

    def close(self):
        pass


def test_release_after_dynamic_entry_is_success():
    events = []
    instance = Device(None)
    with (
        patch("worker.main.NetmikoSwitchSession", return_value=instance),
        patch("worker.main.build_connection_settings_for_switch", return_value=SimpleNamespace()),
        patch("worker.main.legacy_encrypted_secret", side_effect=lambda value: value),
    ):
        device_task(
            {"target_ip": "10.1.2.3", "kind": "release", "force": False},
            dependency(),
            lambda **data: events.append(data),
        )
    assert instance.release_calls == 1
    assert events[-1]["status"] == "succeeded"
    assert events[-1]["after_state"]["entry_type"] == "dynamic"


def test_prequery_never_sends_release_command():
    events = []
    instance = Device(None)
    with (
        patch("worker.main.NetmikoSwitchSession", return_value=instance),
        patch("worker.main.build_connection_settings_for_switch", return_value=SimpleNamespace()),
        patch("worker.main.legacy_encrypted_secret", side_effect=lambda value: value),
    ):
        device_task(
            {"target_ip": "10.1.2.3", "kind": "pre_release_query", "force": False},
            dependency(),
            lambda **data: events.append(data),
        )
    assert instance.release_calls == 0
    assert events[-1]["status"] == "waiting_confirmation"
