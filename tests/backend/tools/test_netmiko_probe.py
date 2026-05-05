from bind_plane.tools.netmiko_probe import ProbeResult, ProbeStep, redact, to_jsonable


def test_redact_replaces_known_secrets() -> None:
    assert redact("password=secret", ["secret"]) == "password=***REDACTED***"


def test_probe_result_json_shape() -> None:
    result = ProbeResult(
        ok=True,
        target={"host": "127.0.0.1", "port": 10023},
        prompt="<switch>",
        steps=[ProbeStep(name="connect", ok=True)],
    )

    assert to_jsonable(result)["steps"] == [
        {"name": "connect", "ok": True, "output": None, "error": None}
    ]
