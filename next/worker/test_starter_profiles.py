"""The starter command profiles, as migrated, read real switch replies correctly."""

import json
import os
from ipaddress import IPv4Address
from pathlib import Path

import asyncpg
import pytest
from bind_plane.services.command_profiles import parse_arp_observation, render_commands

SAMPLES = json.loads((Path(__file__).parent / "testdata" / "arp_samples.json").read_text())["cases"]


def _json(value):
    return json.loads(value) if isinstance(value, str) else value


@pytest.mark.asyncio
async def test_starter_profiles_read_captured_switch_replies():
    dsn = os.getenv("EXECUTOR_TEST_DATABASE_URL")
    if not dsn:
        pytest.skip("isolated migrated test database required")
    conn = await asyncpg.connect(dsn)
    try:
        rows = await conn.fetch(
            "SELECT seed,vendor,command_templates,parser_rules,pagination_rules"
            " FROM bp_command_profiles WHERE seed IS NOT NULL"
        )
    finally:
        await conn.close()
    profiles = {row["seed"]: row for row in rows}
    shipped = {"h3c-comware", "huawei-vrp", "huawei-ce", "huawei-quidway", "ruijie-rgos"}
    assert set(profiles) == shipped

    for row in rows:
        commands = render_commands(
            _json(row["command_templates"]),
            IPv4Address("192.0.2.1"),
            pagination_rules=_json(row["pagination_rules"]),
        )
        assert "192.0.2.1" in commands.query_before and "192.0.2.1" in commands.release

    for case in SAMPLES:
        for key in case["profiles"]:
            observation = parse_arp_observation(
                target_ip=IPv4Address(case["target"]),
                output=case["output"],
                parser_rules=_json(profiles[key]["parser_rules"]),
            )
            assert observation.entry_type == case["expect"], f"{key}: {case['note']}"
