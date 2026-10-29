from types import SimpleNamespace
from unittest.mock import AsyncMock, patch

import pytest
from worker.main import dependency_problem, device_task, execute

PINNED = {"switch_version": 1, "command_profile_version": 1}


def dependency():
    return {
        "is_enabled": True,
        "credential_active": True,
        "profile_active": True,
        "vendor": "huawei",
        "protocol": "telnet",
        "port": None,
        "switch_version": 1,
        "profile_version": 1,
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
        patch("worker.main.connection_settings", return_value=SimpleNamespace()),
        patch("worker.main.legacy_encrypted_secret", side_effect=lambda value: value),
    ):
        device_task(
            {"target_ip": "10.1.2.3", "kind": "release", "force": False, **PINNED},
            dependency(),
            lambda **data: events.append(data),
        )
    assert instance.release_calls == 1
    assert events[-1]["status"] == "succeeded"
    assert events[-1]["after_state"]["entry_type"] == "dynamic"


def test_dynamic_record_never_sends_release_command():
    class DynamicDevice(Device):
        def query_before(self, commands):
            return "10.1.2.3 aa:bb dynamic"

    events = []
    instance = DynamicDevice(None)
    with (
        patch("worker.main.NetmikoSwitchSession", return_value=instance),
        patch("worker.main.connection_settings", return_value=SimpleNamespace()),
        patch("worker.main.legacy_encrypted_secret", side_effect=lambda value: value),
    ):
        device_task(
            {"target_ip": "10.1.2.3", "kind": "release", "force": False, **PINNED},
            dependency(),
            lambda **data: events.append(data),
        )
    assert instance.release_calls == 0
    assert events[-1]["status"] == "cancelled"


def test_prequery_never_sends_release_command():
    events = []
    instance = Device(None)
    with (
        patch("worker.main.NetmikoSwitchSession", return_value=instance),
        patch("worker.main.connection_settings", return_value=SimpleNamespace()),
        patch("worker.main.legacy_encrypted_secret", side_effect=lambda value: value),
    ):
        device_task(
            {"target_ip": "10.1.2.3", "kind": "pre_release_query", "force": False, **PINNED},
            dependency(),
            lambda **data: events.append(data),
        )
    assert instance.release_calls == 0
    assert events[-1]["status"] == "waiting_confirmation"


def test_changed_revision_never_reaches_the_switch():
    job = {"target_ip": "10.1.2.3", "kind": "release", "force": False, **PINNED}
    edited = {**dependency(), "switch_version": 2}
    assert dependency_problem(job, edited).startswith("Switch configuration changed")
    assert dependency_problem(job, {**dependency(), "profile_version": 3}) is not None
    assert dependency_problem(job, {**dependency(), "is_enabled": False}) is not None
    assert dependency_problem(job, dependency()) is None
    with patch("worker.main.NetmikoSwitchSession") as session, pytest.raises(ValueError):
        device_task(job, edited, lambda **data: None)
    session.assert_not_called()


def test_unusable_configuration_is_a_definite_failure():
    events = []
    with (
        patch("worker.main.NetmikoSwitchSession") as session,
        patch("worker.main.legacy_encrypted_secret", side_effect=ValueError("bad key")),
    ):
        device_task(
            {"target_ip": "10.1.2.3", "kind": "release", "force": False, **PINNED},
            dependency(),
            lambda **data: events.append(data),
        )
    session.assert_not_called()
    assert events[-1]["status"] == "failed"
    assert "bad key" not in events[-1]["error_message"]


@pytest.mark.asyncio
async def test_precondition_failure_does_not_block_the_address():
    job = {"id": "job", "target_ip": "10.1.2.3", "kind": "release", "force": False, **PINNED}
    saved = AsyncMock()
    edited = AsyncMock(return_value={**dependency(), "switch_version": 2})
    with (
        patch("worker.main.heartbeat", AsyncMock()),
        patch("worker.main.checkpoint", saved),
        patch("worker.main.load_dependencies", edited),
        patch("worker.main.device_task") as device,
    ):
        await execute(None, job)
    device.assert_not_called()
    assert saved.await_args.kwargs["status"] == "failed"


@pytest.mark.asyncio
async def test_dependency_load_failure_is_not_uncertain():
    job = {"id": "job", "target_ip": "10.1.2.3", "kind": "release", "force": False, **PINNED}
    saved = AsyncMock()
    with (
        patch("worker.main.heartbeat", AsyncMock()),
        patch("worker.main.checkpoint", saved),
        patch("worker.main.load_dependencies", AsyncMock(side_effect=OSError("db"))),
    ):
        await execute(None, job)
    assert saved.await_args.kwargs["status"] == "failed"
