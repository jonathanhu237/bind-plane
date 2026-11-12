import re
from contextlib import suppress
from dataclasses import dataclass
from typing import Protocol

from netmiko import ConnectHandler
from netmiko.exceptions import NetmikoAuthenticationException, NetmikoTimeoutException, ReadTimeout

from bind_plane.services.command_profiles import RenderedCommands

# The end of a switch's answer to one line: a whole prompt on a line of its own (<name>, [name],
# [~name], name#, name>, name(config)#) or a question it waits on (Password:, [Y/N]). Anchored at
# the end of what has arrived; a lone ">" left over from the sign-in is not a prompt.
STEP_END_PATTERN = (
    r"(?:(?:^|[\r\n])[ \t]*(?:<[^<>\r\n]+>|\[[^\[\]\r\n]+\]|[\w.()/~*-]+[#>])"
    r"|[Pp]assword:|\[[Yy]/[Nn]\]:?)\s*$"
)

# Login questions as a switch asks them: at the end of what has arrived and ending in a colon, so
# a banner such as Huawei's "Login authentication" is not taken for a question.
USERNAME_PROMPT = r"(?:user ?name|login|user)\s*:\s*$"
PASSWORD_PROMPT = r"password\s*:\s*$"


def huawei_telnet_login(
    connection, *, username_pattern: str | None = None, pwd_pattern: str | None = None, **_
) -> str:
    """Huawei's Telnet login, for switches that ask for a username and a password and for those
    that ask only for a password. Netmiko's own takes the "Login authentication" banner for a
    username question, sends an empty name the switch rejects as a wrong password, and then
    types the password before the switch asks again, so it is lost and the login times out."""
    username = username_pattern or USERNAME_PROMPT
    password = pwd_pattern or PASSWORD_PROMPT
    try:
        output = connection.read_until_pattern(
            pattern=f"(?:{username})|(?:{password})", re_flags=re.I
        )
        transcript = output
        if not re.search(password, output, flags=re.I):
            connection.write_channel((connection.username or "") + "\r")
            output = connection.read_until_pattern(pattern=password, re_flags=re.I)
            transcript += output
        # Some old Quidway switches reject the first answer to the password question whatever it
        # is, and accept the same password when they ask again; a third question means it is wrong.
        for _ in range(2):
            connection.write_channel(connection.password + "\r")
            output = connection.read_until_pattern(
                pattern=f"(?:{connection.prompt_or_password_change})|(?:{password})", re_flags=re.I
            )
            transcript += output
            asked_again = re.search(password, output, flags=re.I) and not re.search(
                connection.prompt_pattern, output
            )
            if not asked_again:
                break
        else:
            raise NetmikoAuthenticationException(f"Login failed: {connection.host}")
        if re.search(connection.password_change_prompt, output):
            # "The password needs to be changed. Change now? [Y/N]": leave it to its owner.
            connection.write_channel("N" + connection.TELNET_RETURN)
            transcript += connection.read_until_pattern(pattern=connection.prompt_pattern)
        return transcript
    except (EOFError, ReadTimeout) as exc:
        raise NetmikoAuthenticationException(f"Login failed: {connection.host}") from exc


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
        if self._huawei_telnet():

            def original_telnet_login(*args, **kwargs):
                return huawei_telnet_login(connection, **kwargs)
        else:
            original_telnet_login = connection.telnet_login

        def telnet_login_with_profile_patterns(*args, **kwargs):
            kwargs.update(login_patterns)
            return original_telnet_login(*args, **kwargs)

        connection.telnet_login = telnet_login_with_profile_patterns
        connection._open()

    def _huawei_telnet(self) -> bool:
        return self.settings.device_type.startswith(
            "huawei"
        ) and self.settings.device_type.endswith("_telnet")

    def connect(self) -> None:
        try:
            has_login_patterns = bool(
                self.settings.username_pattern
                or self.settings.password_pattern
                or self._huawei_telnet()
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

    def _drain(self) -> None:
        """Discard what the switch printed after the last answer was read, such as the end of
        the sign-in prompt or a log message, so the next answer is not mistaken for it."""
        connection = self._require_connection()
        with suppress(NetmikoTimeoutException, ReadTimeout, OSError):
            connection.clear_buffer(backoff_max=0.4)

    def _send_step(self, command: str, *, echo: bool = True) -> str:
        """Send one line of a release and return as soon as the switch answers with a prompt
        (<name>, [name], [~name], name#, name(config)#) or asks a question (Password:, [Y/N]).
        Waiting until the switch fell silent instead cost two seconds a line. The privilege
        password is not echoed, so it is sent with echo=False."""
        connection = self._require_connection()
        try:
            return connection.send_command(
                command,
                expect_string=STEP_END_PATTERN,
                read_timeout=self.settings.timeout,
                strip_prompt=False,
                strip_command=False,
                cmd_verify=echo,
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

    def elevate_privilege(
        self, command: str, output: list[str] | None = None, *, required: bool = True
    ) -> list[str]:
        """Enter the privileged level (super on H3C and Huawei, enable on Ruijie), answering the
        privilege password when the switch asks. The password never appears in the output.

        When the switch asks but the credential has no privilege password, a release fails here
        with a clear reason; a read (required=False) answers with nothing until the switch gives
        up and goes on at the level it signed in at, as jbxt did for switches it never elevated."""
        output = [] if output is None else output
        self._drain()
        privilege_output = self._send_step(command)
        output.append(privilege_output)
        if "assword" in privilege_output:
            if not self.settings.secret:
                if required:
                    raise SwitchAuthenticationError("Switch privilege password is required")
                for _ in range(3):
                    answer = self._send_step("", echo=False)
                    output.append(answer)
                    if not re.search(r"assword:\s*$", answer):
                        break
                return output
            secret_output = self._send_step(self.settings.secret, echo=False)
            output.append(secret_output.replace(self.settings.secret, "***REDACTED***"))
        return output

    def release(self, commands: RenderedCommands) -> str:
        output: list[str] = []
        try:
            if commands.elevate_privilege:
                self.elevate_privilege(commands.elevate_privilege, output)
            if commands.enter_config:
                output.append(self._send_step(commands.enter_config))
            if self.settings.release_expect_string:
                output.append(
                    self._send_command(
                        commands.release,
                        expect_string=self.settings.release_expect_string,
                    )
                )
            else:
                output.append(self._send_step(commands.release))
            if commands.exit_config:
                output.append(self._send_step(commands.exit_config))
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
