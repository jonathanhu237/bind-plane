import pytest
from bind_plane.services.command_profiles import RenderedCommands
from bind_plane.worker.sessions import (
    NetmikoConnectionSettings,
    NetmikoSwitchSession,
    SwitchSessionError,
)


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
    ) -> str:
        self.calls.append(("send_command", command, expect_string))
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
        ("send_command_timing", "super", None),
        ("send_command_timing", "secret", None),
        ("send_command_timing", "sy", None),
        ("send_command_timing", "undo arp 10.44.132.254", None),
        ("send_command_timing", "return", None),
        ("send_command", "display arp 10.44.132.254", None),
    ]
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
        def send_command_timing(
            self,
            command: str,
            *,
            read_timeout: int,
            strip_prompt: bool,
            strip_command: bool,
        ) -> str:
            if command == "return":
                raise OSError("connection dropped")
            return super().send_command_timing(
                command,
                read_timeout=read_timeout,
                strip_prompt=strip_prompt,
                strip_command=strip_command,
            )

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
        "timing:sy:20:False:False\n"
        "timing:undo arp 10.44.132.254:20:False:False"
    )
