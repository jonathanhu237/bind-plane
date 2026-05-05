from dataclasses import dataclass
from typing import Protocol

from netmiko import ConnectHandler
from netmiko.exceptions import NetmikoAuthenticationException, NetmikoTimeoutException, ReadTimeout

from bind_plane.services.command_profiles import RenderedCommands


class SwitchSessionError(RuntimeError):
    def __init__(self, message: str, *, partial_output: str | None = None) -> None:
        super().__init__(message)
        self.partial_output = partial_output


class SwitchAuthenticationError(SwitchSessionError):
    pass


class SwitchConnectionTimeoutError(SwitchSessionError):
    pass


class SwitchSession(Protocol):
    def connect(self) -> None:
        pass

    def query_before(self, commands: RenderedCommands) -> str:
        pass

    def release(self, commands: RenderedCommands) -> str:
        pass

    def query_after(self, commands: RenderedCommands) -> str:
        pass

    def close(self) -> None:
        pass


@dataclass(frozen=True)
class NetmikoConnectionSettings:
    host: str
    port: int
    device_type: str
    username: str
    password: str
    secret: str | None = None
    timeout: int = 30
    conn_timeout: int = 20
    auth_timeout: int = 20
    banner_timeout: int = 20
    global_delay_factor: float = 1.0
    username_pattern: str | None = None
    password_pattern: str | None = None
    passphrase_pattern: str | None = None
    query_before_expect_string: str | None = None
    query_after_expect_string: str | None = None
    release_expect_string: str | None = None


class NetmikoSwitchSession:
    def __init__(self, settings: NetmikoConnectionSettings) -> None:
        self.settings = settings
        self._connection = None

    def _connection_kwargs(self, *, auto_connect: bool) -> dict:
        return {
            "device_type": self.settings.device_type,
            "host": self.settings.host,
            "port": self.settings.port,
            "username": self.settings.username,
            "password": self.settings.password,
            "secret": self.settings.secret or "",
            "timeout": self.settings.timeout,
            "conn_timeout": self.settings.conn_timeout,
            "auth_timeout": self.settings.auth_timeout,
            "banner_timeout": self.settings.banner_timeout,
            "global_delay_factor": self.settings.global_delay_factor,
            "fast_cli": False,
            "auto_connect": auto_connect,
        }

    def _open_with_login_patterns(self, connection) -> None:
        login_patterns = {
            key: value
            for key, value in {
                "username_pattern": self.settings.username_pattern,
                "pwd_pattern": self.settings.password_pattern,
            }.items()
            if value
        }
        original_telnet_login = connection.telnet_login

        def telnet_login_with_profile_patterns(*args, **kwargs):
            kwargs.update(login_patterns)
            return original_telnet_login(*args, **kwargs)

        connection.telnet_login = telnet_login_with_profile_patterns
        connection._open()

    def connect(self) -> None:
        try:
            has_login_patterns = bool(
                self.settings.username_pattern or self.settings.password_pattern
            )
            self._connection = ConnectHandler(
                **self._connection_kwargs(auto_connect=not has_login_patterns)
            )
            if has_login_patterns:
                self._open_with_login_patterns(self._connection)
        except NetmikoAuthenticationException as exc:
            raise SwitchAuthenticationError("Switch authentication failed") from exc
        except NetmikoTimeoutException as exc:
            raise SwitchConnectionTimeoutError("Switch connection timed out") from exc
        except OSError as exc:
            raise SwitchSessionError("Switch connection failed") from exc

    def _require_connection(self):
        if self._connection is None:
            raise SwitchSessionError("Switch session is not connected")
        return self._connection

    def _send_command(self, command: str, *, expect_string: str | None = None) -> str:
        connection = self._require_connection()
        try:
            kwargs = {"read_timeout": self.settings.timeout}
            if expect_string:
                kwargs["expect_string"] = expect_string
            return connection.send_command(command, **kwargs)
        except (NetmikoTimeoutException, ReadTimeout) as exc:
            raise SwitchConnectionTimeoutError("Switch command timed out") from exc
        except OSError as exc:
            raise SwitchSessionError("Switch command failed") from exc

    def _send_timing(self, command: str) -> str:
        connection = self._require_connection()
        try:
            return connection.send_command_timing(
                command,
                read_timeout=self.settings.timeout,
                strip_prompt=False,
                strip_command=False,
            )
        except (NetmikoTimeoutException, ReadTimeout) as exc:
            raise SwitchConnectionTimeoutError("Switch command timed out") from exc
        except OSError as exc:
            raise SwitchSessionError("Switch command failed") from exc

    def _join_output(self, output: list[str]) -> str | None:
        transcript = "\n".join(part for part in output if part)
        return transcript or None

    def _attach_partial_output(self, exc: SwitchSessionError, output: list[str]) -> None:
        if exc.partial_output:
            return
        exc.partial_output = self._join_output(output)

    def query_before(self, commands: RenderedCommands) -> str:
        connection = self._require_connection()
        try:
            if commands.disable_paging:
                connection.disable_paging(command=commands.disable_paging)
        except (NetmikoTimeoutException, ReadTimeout) as exc:
            raise SwitchConnectionTimeoutError("Switch paging command timed out") from exc
        except OSError as exc:
            raise SwitchSessionError("Switch paging command failed") from exc
        return self._send_command(
            commands.query_before,
            expect_string=self.settings.query_before_expect_string,
        )

    def release(self, commands: RenderedCommands) -> str:
        output: list[str] = []
        try:
            if commands.elevate_privilege:
                privilege_output = self._send_timing(commands.elevate_privilege)
                output.append(privilege_output)
                if "assword" in privilege_output:
                    if not self.settings.secret:
                        raise SwitchAuthenticationError("Switch privilege password is required")
                    secret_output = self._send_timing(self.settings.secret)
                    output.append(secret_output.replace(self.settings.secret, "***REDACTED***"))
            if commands.enter_config:
                output.append(self._send_timing(commands.enter_config))
            if self.settings.release_expect_string:
                output.append(
                    self._send_command(
                        commands.release,
                        expect_string=self.settings.release_expect_string,
                    )
                )
            else:
                output.append(self._send_timing(commands.release))
            if commands.exit_config:
                output.append(self._send_timing(commands.exit_config))
        except SwitchSessionError as exc:
            self._attach_partial_output(exc, output)
            raise
        return self._join_output(output) or ""

    def query_after(self, commands: RenderedCommands) -> str:
        return self._send_command(
            commands.query_after,
            expect_string=self.settings.query_after_expect_string,
        )

    def close(self) -> None:
        if self._connection is not None:
            self._connection.disconnect()
            self._connection = None
