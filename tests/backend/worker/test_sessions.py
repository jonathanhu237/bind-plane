import pytest
from bind_plane.services.command_profiles import RenderedCommands
from bind_plane.worker.sessions import (
    STEP_END_PATTERN,
    NetmikoConnectionSettings,
    NetmikoSwitchSession,
    SwitchSessionError,
    huawei_telnet_login,
)
from netmiko.exceptions import NetmikoAuthenticationException


class FakeConnection:
    def __init__(self) -> None:
        self.calls: list[tuple[str, str, str | None]] = []
        self.login_calls: list[dict] = []
        self.closed = False

    def _open(self) -> None:
        self.telnet_login()

    def telnet_login(self, **kwargs) -> str:
        self.login_calls.append(kwargs)
        return "login complete"

    def disable_paging(self, *, command: str) -> str:
        self.calls.append(("disable_paging", command, None))
        return "paging disabled"

    def send_command(
        self,
        command: str,
        *,
        read_timeout: int,
        expect_string: str | None = None,
        cmd_verify: bool = True,
        **kwargs,
    ) -> str:
        self.calls.append(("send_command", command, expect_string))
        if expect_string == STEP_END_PATTERN:
            # A release step: the switch answers with a prompt or a question.
            self.unechoed = (
                [*getattr(self, "unechoed", []), command]
                if not cmd_verify
                else getattr(self, "unechoed", [])
            )
            if command == "super":
                return "Password:"
            if command == "secret":
                return "secret\nUser privilege level is 3\n<edge-sw-01>"
            return f"{command}\n[edge-sw-01]"
        return f"output:{command}:{read_timeout}"

    def send_command_timing(
        self,
        command: str,
        *,
        read_timeout: int,
        strip_prompt: bool,
        strip_command: bool,
    ) -> str:
        self.calls.append(("send_command_timing", command, None))
        if command == "super":
            return "Password:"
        if command == "secret":
            return "secret\nUser privilege level is 3"
        return f"timing:{command}:{read_timeout}:{strip_prompt}:{strip_command}"

    def disconnect(self) -> None:
        self.closed = True


def test_netmiko_session_uses_explicit_profile_config_sequence(monkeypatch) -> None:
    fake = FakeConnection()

    def connect_handler(**kwargs):
        assert kwargs["secret"] == "secret"
        assert kwargs["auto_connect"] is True
        return fake

    monkeypatch.setattr("bind_plane.worker.sessions.ConnectHandler", connect_handler)
    session = NetmikoSwitchSession(
        NetmikoConnectionSettings(
            host="127.0.0.1",
            port=10023,
            device_type="hp_comware_telnet",
            username="monitor",
            password="password",
            secret="secret",
            timeout=20,
        )
    )

    session.connect()
    before = session.query_before(
        RenderedCommands(
            query_before="display arp 10.44.132.254",
            release="undo arp 10.44.132.254",
            query_after="display arp 10.44.132.254",
            disable_paging="screen-length disable",
        )
    )
    release = session.release(
        RenderedCommands(
            query_before="display arp 10.44.132.254",
            release="undo arp 10.44.132.254",
            query_after="display arp 10.44.132.254",
            elevate_privilege="super",
            enter_config="sy",
            exit_config="return",
        )
    )
    after = session.query_after(
        RenderedCommands(
            query_before="display arp 10.44.132.254",
            release="undo arp 10.44.132.254",
            query_after="display arp 10.44.132.254",
        )
    )
    session.close()

    assert before == "output:display arp 10.44.132.254:20"
    assert after == "output:display arp 10.44.132.254:20"
    assert "***REDACTED***" in release
    assert "secret\n" not in release
    assert fake.calls == [
        ("disable_paging", "screen-length disable", None),
        ("send_command", "display arp 10.44.132.254", None),
        ("send_command", "super", STEP_END_PATTERN),
        ("send_command", "secret", STEP_END_PATTERN),
        ("send_command", "sy", STEP_END_PATTERN),
        ("send_command", "undo arp 10.44.132.254", STEP_END_PATTERN),
        ("send_command", "return", STEP_END_PATTERN),
        ("send_command", "display arp 10.44.132.254", None),
    ]
    # The privilege password is never echoed, so nothing waits for its echo.
    assert fake.unechoed == ["secret"]
    assert fake.closed is True


def test_netmiko_session_uses_prompt_patterns(monkeypatch) -> None:
    fake = FakeConnection()

    def connect_handler(**kwargs):
        assert "username_pattern" not in kwargs
        assert "password_pattern" not in kwargs
        assert kwargs["auto_connect"] is False
        return fake

    monkeypatch.setattr("bind_plane.worker.sessions.ConnectHandler", connect_handler)
    session = NetmikoSwitchSession(
        NetmikoConnectionSettings(
            host="127.0.0.1",
            port=10023,
            device_type="hp_comware_telnet",
            username="monitor",
            password="password",
            timeout=20,
            username_pattern="Username:",
            password_pattern="Password:",
            query_before_expect_string=r"<edge-sw-01>",
            query_after_expect_string=r"<edge-sw-01>",
            release_expect_string=r"\[edge-sw-01\]",
        )
    )

    session.connect()
    commands = RenderedCommands(
        query_before="display arp 10.44.132.254",
        release="undo arp 10.44.132.254",
        query_after="display arp 10.44.132.254",
    )
    session.query_before(commands)
    session.release(commands)
    session.query_after(commands)

    assert fake.login_calls == [{"username_pattern": "Username:", "pwd_pattern": "Password:"}]
    assert fake.calls == [
        ("send_command", "display arp 10.44.132.254", r"<edge-sw-01>"),
        ("send_command", "undo arp 10.44.132.254", r"\[edge-sw-01\]"),
        ("send_command", "display arp 10.44.132.254", r"<edge-sw-01>"),
    ]


def test_netmiko_session_attaches_partial_release_output(monkeypatch) -> None:
    class FailingExitConnection(FakeConnection):
        def send_command(self, command: str, **kwargs) -> str:
            if command == "return":
                raise OSError("connection dropped")
            return super().send_command(command, **kwargs)

    fake = FailingExitConnection()
    monkeypatch.setattr("bind_plane.worker.sessions.ConnectHandler", lambda **_: fake)
    session = NetmikoSwitchSession(
        NetmikoConnectionSettings(
            host="127.0.0.1",
            port=10023,
            device_type="hp_comware_telnet",
            username="monitor",
            password="password",
            timeout=20,
        )
    )
    session.connect()

    with pytest.raises(SwitchSessionError) as exc_info:
        session.release(
            RenderedCommands(
                query_before="display arp 10.44.132.254",
                release="undo arp 10.44.132.254",
                query_after="display arp 10.44.132.254",
                enter_config="sy",
                exit_config="return",
            )
        )

    assert exc_info.value.partial_output == (
        "sy\n[edge-sw-01]\nundo arp 10.44.132.254\n[edge-sw-01]"
    )


class FakeHuaweiTelnet:
    """A Huawei switch on Telnet, replying the way the campus's do (seen 2026-11-12)."""

    from netmiko.huawei.huawei import HuaweiTelnet as _H

    prompt_pattern = _H.prompt_pattern
    password_change_prompt = _H.password_change_prompt
    prompt_or_password_change = _H.prompt_or_password_change
    TELNET_RETURN = "\r\n"
    host = "10.0.0.1"

    def __init__(self, greeting, replies, username="", password="pw"):
        self.buffer = greeting
        self.replies = replies
        self.username = username
        self.password = password
        self.writes = []

    def read_until_pattern(self, pattern, re_flags=0, read_timeout=10):
        import re

        from netmiko.exceptions import ReadTimeout

        found = re.search(pattern, self.buffer, flags=re_flags)
        if not found:
            raise ReadTimeout("pattern not found")
        output, self.buffer = self.buffer[: found.end()], self.buffer[found.end() :]
        return output

    def write_channel(self, data):
        self.writes.append(data)
        self.buffer += self.replies.get(data, "\r\nError: The password is invalid.\r\nPassword:")


BANNER = "Warning: Telnet is not a secure protocol, and it is recommended to use Stelnet.\r\n"
WELCOME = "\r\nInfo: The max number of VTY users is 10.\r\n<SW>"


def test_huawei_login_answers_a_password_only_switch() -> None:
    # The banner says "Login authentication"; that is no question, so nothing is sent for it.
    switch = FakeHuaweiTelnet(
        BANNER + "Login authentication\r\n\r\nPassword:",
        {"pw\r": "\r\nWarning: The password of the configuration is too weak." + WELCOME},
    )
    huawei_telnet_login(switch)
    assert switch.writes == ["pw\r"]


def test_huawei_login_answers_username_then_password() -> None:
    switch = FakeHuaweiTelnet(
        "Login authentication\r\n\r\nUsername:",
        {"admin\r": "\r\nPassword:", "pw\r": WELCOME},
        username="admin",
    )
    huawei_telnet_login(switch)
    assert switch.writes == ["admin\r", "pw\r"]


def test_huawei_login_declines_a_password_change_and_fails_fast_on_a_wrong_password() -> None:
    changing = FakeHuaweiTelnet(
        "Password:",
        {
            "pw\r": "\r\nThe password needs to be changed. Change now? [Y/N]:",
            "N\r\n": WELCOME,
        },
    )
    huawei_telnet_login(changing)
    assert changing.writes == ["pw\r", "N\r\n"]

    wrong = FakeHuaweiTelnet("Password:", {}, password="wrong")
    with pytest.raises(NetmikoAuthenticationException):
        huawei_telnet_login(wrong)
    assert wrong.writes == ["wrong\r"]
