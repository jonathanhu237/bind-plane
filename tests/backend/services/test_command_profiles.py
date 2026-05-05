from ipaddress import IPv4Address

from bind_plane.db.models import ArpEntryType, ReleaseJobStatus
from bind_plane.services.command_profiles import (
    CommandProfileError,
    classify_release_result,
    parse_arp_observation,
    render_commands,
)


def test_render_commands_uses_profile_templates() -> None:
    commands = render_commands(
        {
            "single_arp_query": "display arp $ip",
            "arp_release": "undo arp $ip",
            "super": "super",
            "config": "sy",
            "exit_config": "return",
            "disable_paging": "screen-length disable",
        },
        IPv4Address("10.44.132.254"),
    )

    assert commands.query_before == "display arp 10.44.132.254"
    assert commands.release == "undo arp 10.44.132.254"
    assert commands.query_after == "display arp 10.44.132.254"
    assert commands.elevate_privilege == "super"
    assert commands.enter_config == "sy"
    assert commands.exit_config == "return"
    assert commands.disable_paging == "screen-length disable"


def test_render_commands_uses_pagination_rules_for_disable_paging() -> None:
    commands = render_commands(
        {
            "single_arp_query": "display arp $ip",
            "arp_release": "undo arp $ip",
        },
        IPv4Address("10.44.132.254"),
        pagination_rules={"disable_paging_command": "screen-length disable"},
    )

    assert commands.disable_paging == "screen-length disable"


def test_render_commands_rejects_unknown_placeholders() -> None:
    try:
        render_commands(
            {
                "single_arp_query": "display arp $target_ip",
                "arp_release": "undo arp $ip",
            },
            IPv4Address("10.44.132.254"),
        )
    except CommandProfileError as exc:
        assert (
            str(exc)
            == "Unsupported command template placeholder(s) in single_arp_query: target_ip"
        )
        return
    raise AssertionError("Expected CommandProfileError")


def test_parse_h3c_arp_output_classifies_static_entry() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output=(
            "Type: S-Static    D-Dynamic    M-Multiport\n"
            "IP Address       MAC Address     VLAN ID  Interface Aging Type\n"
            "10.44.132.254    0011-2233-4455  100      GE1/0/1   N/A   S\n"
        ),
        parser_rules={
            "arp_entry_regex": (
                r"(?P<ip>10\.44\.132\.254)\s+"
                r"(?P<mac>[0-9a-f-]+)\s+\S+\s+\S+\s+\S+\s+(?P<type>\S+)"
            ),
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    assert observation.entry_type == ArpEntryType.STATIC
    assert observation.mac == "0011-2233-4455"


def test_classify_release_succeeds_when_dynamic_entry_remains() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="10.44.132.254    0011-2233-4455  100      GE1/0/1   20    D",
        parser_rules={
            "arp_entry_regex": (
                r"(?P<ip>10\.44\.132\.254)\s+"
                r"(?P<mac>[0-9a-f-]+)\s+\S+\s+\S+\s+\S+\s+(?P<type>\S+)"
            ),
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    classified = classify_release_result(
        release_output="undo arp 10.44.132.254",
        after_observation=observation,
        error_patterns=["Error:"],
    )

    assert classified.status == ReleaseJobStatus.SUCCEEDED


def test_classify_release_succeeds_when_final_state_is_clear_even_without_success_pattern() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="No matching ARP entries found",
        parser_rules={
            "missing_patterns": ["No matching"],
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>\S+)\s+(?P<type>\S+)",
        },
    )

    classified = classify_release_result(
        release_output="undo arp 10.44.132.254",
        after_observation=observation,
        error_patterns=["Error:"],
        success_patterns=["Done"],
    )

    assert classified.status == ReleaseJobStatus.SUCCEEDED
    assert classified.details["success_pattern_matched"] is False


def test_parse_missing_pattern_classifies_missing_record() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="No matching ARP entries found",
        parser_rules={
            "missing_patterns": ["No matching"],
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>\S+)\s+(?P<type>\S+)",
        },
    )

    assert observation.entry_type == ArpEntryType.MISSING
    assert observation.mac is None


def test_parse_target_row_before_missing_pattern() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output=(
            "No matching ARP entry on secondary table\n"
            "10.44.132.254    0011-2233-4455  S\n"
        ),
        parser_rules={
            "missing_patterns": ["No matching"],
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    assert observation.entry_type == ArpEntryType.STATIC
    assert observation.mac == "0011-2233-4455"


def test_parse_prefers_static_when_multiple_target_rows_exist() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output=(
            "10.44.132.254    aaaa-bbbb-cccc  D\n"
            "10.44.132.254    0011-2233-4455  S\n"
        ),
        parser_rules={
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    assert observation.entry_type == ArpEntryType.STATIC
    assert observation.mac == "0011-2233-4455"


def test_parse_treats_unknown_target_row_as_higher_risk_than_dynamic() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output=(
            "10.44.132.254    aaaa-bbbb-cccc  D\n"
            "10.44.132.254    0011-2233-4455  weird\n"
        ),
        parser_rules={
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    assert observation.entry_type == ArpEntryType.UNKNOWN
    assert observation.mac == "0011-2233-4455"


def test_parse_without_rules_marks_unknown() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="unstructured switch output",
        parser_rules={},
    )

    assert observation.entry_type == ArpEntryType.UNKNOWN


def test_parse_non_matching_output_marks_unknown_not_missing() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="unstructured switch output",
        parser_rules={
            "missing_patterns": ["No matching"],
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
        },
    )

    classified = classify_release_result(
        release_output="undo arp 10.44.132.254",
        after_observation=observation,
        error_patterns=["Error:"],
    )

    assert observation.entry_type == ArpEntryType.UNKNOWN
    assert classified.status == ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION


def test_parse_ignores_non_target_ip_matches() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output=(
            "10.44.132.1    aaaa-bbbb-cccc  S\n"
            "10.44.132.2    dddd-eeee-ffff  D\n"
        ),
        parser_rules={
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    assert observation.entry_type == ArpEntryType.UNKNOWN
    assert observation.mac is None


def test_parse_finds_target_ip_after_non_target_matches() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output=(
            "10.44.132.1      aaaa-bbbb-cccc  S\n"
            "10.44.132.254    0011-2233-4455  D\n"
        ),
        parser_rules={
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    assert observation.entry_type == ArpEntryType.DYNAMIC
    assert observation.mac == "0011-2233-4455"


def test_invalid_regex_patterns_raise_command_profile_error() -> None:
    for parser_rules in (
        {"missing_patterns": ["["]},
        {"arp_entry_regex": "["},
    ):
        try:
            parse_arp_observation(
                target_ip=IPv4Address("10.44.132.254"),
                output="10.44.132.254 0011-2233-4455 S",
                parser_rules=parser_rules,
            )
        except CommandProfileError:
            continue
        raise AssertionError("Expected CommandProfileError")


def test_invalid_result_pattern_raises_command_profile_error() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="10.44.132.254    0011-2233-4455  D",
        parser_rules={
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    for patterns in (
        {"error_patterns": ["["]},
        {"error_patterns": ["Error:"], "success_patterns": ["["]},
    ):
        try:
            classify_release_result(
                release_output="undo arp 10.44.132.254",
                after_observation=observation,
                **patterns,
            )
        except CommandProfileError:
            continue
        raise AssertionError("Expected CommandProfileError")


def test_classify_release_fails_when_static_entry_remains() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="10.44.132.254    0011-2233-4455  S",
        parser_rules={
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    classified = classify_release_result(
        release_output="undo arp 10.44.132.254",
        after_observation=observation,
        error_patterns=["Error:"],
    )

    assert classified.status == ReleaseJobStatus.FAILED


def test_classify_release_needs_manual_confirmation_for_unknown_after_state() -> None:
    observation = parse_arp_observation(
        target_ip=IPv4Address("10.44.132.254"),
        output="10.44.132.254    0011-2233-4455  weird",
        parser_rules={
            "arp_entry_regex": r"(?P<ip>\S+)\s+(?P<mac>[0-9a-f-]+)\s+(?P<type>\S+)",
            "static_type_values": ["S"],
            "dynamic_type_values": ["D"],
        },
    )

    classified = classify_release_result(
        release_output="undo arp 10.44.132.254",
        after_observation=observation,
        error_patterns=["Error:"],
    )

    assert classified.status == ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION
