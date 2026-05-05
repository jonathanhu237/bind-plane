import argparse
import getpass
import json
import os
from collections.abc import Sequence
from dataclasses import dataclass, field
from typing import Any

from netmiko import ConnectHandler
from netmiko.exceptions import NetmikoAuthenticationException, NetmikoTimeoutException

DEFAULT_PASSWORD_ENV = "BIND_PLANE_PROBE_PASSWORD"
DEFAULT_SECRET_ENV = "BIND_PLANE_PROBE_SECRET"


@dataclass
class ProbeStep:
    name: str
    ok: bool
    output: str | None = None
    error: str | None = None


@dataclass
class ProbeResult:
    ok: bool
    target: dict[str, Any]
    prompt: str | None = None
    steps: list[ProbeStep] = field(default_factory=list)


def redact(value: str | None, secrets: Sequence[str]) -> str | None:
    if value is None:
        return None
    redacted = value
    for secret in secrets:
        if secret:
            redacted = redacted.replace(secret, "***REDACTED***")
    return redacted


def prompt_secret(prompt: str) -> str:
    return getpass.getpass(prompt)


def read_secret(value: str | None, env_name: str, prompt: str, required: bool) -> str | None:
    if value:
        return value
    from_env = os.getenv(env_name)
    if from_env:
        return from_env
    if required:
        return prompt_secret(prompt)
    return None


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Probe Netmiko Telnet behavior through a local port forward.",
    )
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument(
        "--device-type",
        required=True,
        help="Netmiko device_type, e.g. h3c_comware, hp_comware, huawei, ruijie_os",
    )
    parser.add_argument("--username", default=os.getenv("BIND_PLANE_PROBE_USERNAME"))
    parser.add_argument("--password-env", default=DEFAULT_PASSWORD_ENV)
    parser.add_argument("--secret-env", default=DEFAULT_SECRET_ENV)
    parser.add_argument("--password")
    parser.add_argument("--secret")
    parser.add_argument("--timeout", type=int, default=30)
    parser.add_argument("--conn-timeout", type=int, default=20)
    parser.add_argument("--auth-timeout", type=int, default=20)
    parser.add_argument("--banner-timeout", type=int, default=20)
    parser.add_argument("--global-delay-factor", type=float, default=1.0)
    parser.add_argument("--disable-paging-command")
    parser.add_argument("--query-command", action="append", default=[])
    parser.add_argument("--config-command", action="append", default=[])
    parser.add_argument("--allow-config", action="store_true")
    return parser


def append_step(
    result: ProbeResult,
    *,
    name: str,
    ok: bool,
    output: str | None = None,
    error: str | None = None,
    secrets: Sequence[str],
) -> None:
    result.steps.append(
        ProbeStep(
            name=name,
            ok=ok,
            output=redact(output, secrets),
            error=redact(error, secrets),
        )
    )


def run_probe(args: argparse.Namespace) -> ProbeResult:
    if not args.username:
        raise SystemExit("--username or BIND_PLANE_PROBE_USERNAME is required")
    if args.config_command and not args.allow_config:
        raise SystemExit("--config-command requires --allow-config")

    password = read_secret(
        args.password,
        args.password_env,
        "Switch password: ",
        required=True,
    )
    secret = read_secret(
        args.secret,
        args.secret_env,
        "Switch secret/super password: ",
        required=False,
    )
    secrets = [value for value in (password, secret) if value]

    target = {
        "host": args.host,
        "port": args.port,
        "device_type": args.device_type,
        "username": args.username,
    }
    result = ProbeResult(ok=False, target=target)

    device = {
        "device_type": args.device_type,
        "host": args.host,
        "port": args.port,
        "username": args.username,
        "password": password,
        "secret": secret or "",
        "timeout": args.timeout,
        "conn_timeout": args.conn_timeout,
        "auth_timeout": args.auth_timeout,
        "banner_timeout": args.banner_timeout,
        "global_delay_factor": args.global_delay_factor,
        "fast_cli": False,
    }

    connection = None
    try:
        connection = ConnectHandler(**device)
        append_step(result, name="connect", ok=True, secrets=secrets)

        prompt = connection.find_prompt()
        result.prompt = redact(prompt, secrets)
        append_step(result, name="find_prompt", ok=True, output=prompt, secrets=secrets)

        if args.disable_paging_command:
            output = connection.disable_paging(command=args.disable_paging_command)
            append_step(result, name="disable_paging", ok=True, output=output, secrets=secrets)

        for index, command in enumerate(args.query_command, start=1):
            output = connection.send_command(command, read_timeout=args.timeout)
            append_step(
                result,
                name=f"query_{index}",
                ok=True,
                output=output,
                secrets=secrets,
            )

        if args.config_command:
            output = connection.send_config_set(args.config_command, read_timeout=args.timeout)
            append_step(result, name="config_commands", ok=True, output=output, secrets=secrets)

        result.ok = True
    except (NetmikoAuthenticationException, NetmikoTimeoutException, OSError) as exc:
        append_step(result, name="connect_or_command", ok=False, error=repr(exc), secrets=secrets)
    finally:
        if connection is not None:
            try:
                connection.disconnect()
                append_step(result, name="disconnect", ok=True, secrets=secrets)
            except OSError as exc:
                append_step(result, name="disconnect", ok=False, error=repr(exc), secrets=secrets)

    return result


def to_jsonable(result: ProbeResult) -> dict[str, Any]:
    return {
        "ok": result.ok,
        "target": result.target,
        "prompt": result.prompt,
        "steps": [step.__dict__ for step in result.steps],
    }


def main() -> None:
    parser = build_parser()
    result = run_probe(parser.parse_args())
    print(json.dumps(to_jsonable(result), ensure_ascii=False, indent=2))
    if not result.ok:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
