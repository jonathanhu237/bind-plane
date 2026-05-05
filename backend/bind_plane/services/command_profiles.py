import re
from dataclasses import dataclass
from ipaddress import IPv4Address
from string import Template

from bind_plane.db.models import ArpEntryType, ReleaseJobStatus
from bind_plane.domain.release import ArpObservation


class CommandProfileError(ValueError):
    pass


@dataclass(frozen=True)
class RenderedCommands:
    query_before: str
    release: str
    query_after: str
    elevate_privilege: str | None = None
    enter_config: str | None = None
    exit_config: str | None = None
    disable_paging: str | None = None


@dataclass(frozen=True)
class ClassifiedReleaseResult:
    status: ReleaseJobStatus
    message: str
    details: dict


def _template_required(templates: dict, name: str) -> str:
    value = templates.get(name)
    if not isinstance(value, str) or not value.strip():
        raise CommandProfileError(f"Missing command template: {name}")
    return value


def _template_optional(templates: dict, name: str) -> str | None:
    value = templates.get(name)
    if not isinstance(value, str) or not value.strip():
        return None
    return value


def _pagination_disable_command(pagination_rules: dict | None) -> str | None:
    if pagination_rules is None:
        return None
    for key in ("disable_paging_command", "disable_paging"):
        value = pagination_rules.get(key)
        if isinstance(value, str) and value.strip():
            return value
    return None


def _render_template(template_value: str, name: str, data: dict[str, str]) -> str:
    template = Template(template_value)
    if not template.is_valid():
        raise CommandProfileError(f"Invalid command template placeholder syntax: {name}")

    unsupported = sorted(set(template.get_identifiers()) - set(data))
    if unsupported:
        raise CommandProfileError(
            f"Unsupported command template placeholder(s) in {name}: {', '.join(unsupported)}"
        )
    return template.substitute(data)


def _render_optional_template(
    command_templates: dict,
    name: str,
    data: dict[str, str],
) -> str | None:
    template_value = _template_optional(command_templates, name)
    if template_value is None:
        return None
    return _render_template(template_value, name, data)


def render_commands(
    command_templates: dict,
    target_ip: IPv4Address,
    *,
    pagination_rules: dict | None = None,
) -> RenderedCommands:
    data = {"ip": str(target_ip)}
    query_template = _template_required(command_templates, "single_arp_query")
    release_template = _template_required(command_templates, "arp_release")
    disable_paging_template = _template_optional(
        command_templates,
        "disable_paging",
    ) or _pagination_disable_command(pagination_rules)

    return RenderedCommands(
        query_before=_render_template(query_template, "single_arp_query", data),
        release=_render_template(release_template, "arp_release", data),
        query_after=_render_template(query_template, "single_arp_query", data),
        elevate_privilege=_render_optional_template(command_templates, "super", data),
        enter_config=_render_optional_template(command_templates, "config", data),
        exit_config=_render_optional_template(command_templates, "exit_config", data),
        disable_paging=(
            _render_template(disable_paging_template, "disable_paging", data)
            if disable_paging_template
            else None
        ),
    )


def _regex_search(pattern: str, output: str) -> re.Match[str] | None:
    try:
        return re.search(pattern, output, re.IGNORECASE | re.MULTILINE)
    except re.error as exc:
        raise CommandProfileError(f"Invalid regex pattern: {pattern}") from exc


def _regex_finditer(pattern: str, output: str) -> list[re.Match[str]]:
    try:
        return list(re.finditer(pattern, output, re.IGNORECASE | re.MULTILINE))
    except re.error as exc:
        raise CommandProfileError(f"Invalid regex pattern: {pattern}") from exc


def output_matches_any(output: str, patterns: list[str]) -> bool:
    for pattern in patterns:
        if not isinstance(pattern, str):
            raise CommandProfileError("Regex pattern must be a string")
        if _regex_search(pattern, output):
            return True
    return False


def _entry_type(raw_type: str, parser_rules: dict) -> ArpEntryType:
    static_values = {value.lower() for value in parser_rules.get("static_type_values", [])}
    dynamic_values = {value.lower() for value in parser_rules.get("dynamic_type_values", [])}

    if raw_type.lower() in static_values:
        return ArpEntryType.STATIC
    if raw_type.lower() in dynamic_values:
        return ArpEntryType.DYNAMIC
    return ArpEntryType.UNKNOWN


def parse_arp_observation(
    *,
    target_ip: IPv4Address,
    output: str,
    parser_rules: dict,
) -> ArpObservation:
    entry_regex = parser_rules.get("arp_entry_regex")
    if isinstance(entry_regex, str) and entry_regex.strip():
        target_observations: list[ArpObservation] = []
        for match in _regex_finditer(entry_regex, output):
            groups = match.groupdict()
            if groups.get("ip", "").strip() != str(target_ip):
                continue
            target_observations.append(
                ArpObservation(
                    target_ip=target_ip,
                    entry_type=_entry_type(groups.get("type") or "", parser_rules),
                    mac=groups.get("mac"),
                    raw_output=output,
                )
            )
        for entry_type in (ArpEntryType.STATIC, ArpEntryType.UNKNOWN, ArpEntryType.DYNAMIC):
            for observation in target_observations:
                if observation.entry_type is entry_type:
                    return observation

    missing_patterns = parser_rules.get("missing_patterns", [])
    if isinstance(missing_patterns, list) and output_matches_any(output, missing_patterns):
        return ArpObservation(
            target_ip=target_ip,
            entry_type=ArpEntryType.MISSING,
            mac=None,
            raw_output=output,
        )

    return ArpObservation(
        target_ip=target_ip,
        entry_type=ArpEntryType.UNKNOWN,
        mac=None,
        raw_output=output,
    )


def observation_to_dict(observation: ArpObservation) -> dict:
    return {
        "target_ip": str(observation.target_ip),
        "entry_type": observation.entry_type,
        "mac": observation.mac,
    }


def classify_release_result(
    *,
    release_output: str,
    after_observation: ArpObservation,
    error_patterns: list[str],
    success_patterns: list[str] | None = None,
) -> ClassifiedReleaseResult:
    if output_matches_any(release_output, error_patterns):
        return ClassifiedReleaseResult(
            status=ReleaseJobStatus.FAILED,
            message="Release command returned an error pattern",
            details={"after_state": observation_to_dict(after_observation)},
        )

    success_patterns = success_patterns or []
    success_pattern_matched = (
        output_matches_any(release_output, success_patterns) if success_patterns else None
    )
    if after_observation.entry_type in {ArpEntryType.MISSING, ArpEntryType.DYNAMIC}:
        details = {"after_state": observation_to_dict(after_observation)}
        if success_pattern_matched is not None:
            details["success_pattern_matched"] = success_pattern_matched
        return ClassifiedReleaseResult(
            status=ReleaseJobStatus.SUCCEEDED,
            message="Static binding is not present after release",
            details=details,
        )

    if after_observation.entry_type is ArpEntryType.STATIC:
        return ClassifiedReleaseResult(
            status=ReleaseJobStatus.FAILED,
            message="Static binding is still present after release",
            details={"after_state": observation_to_dict(after_observation)},
        )

    return ClassifiedReleaseResult(
        status=ReleaseJobStatus.NEEDS_MANUAL_CONFIRMATION,
        message="Post-release state could not be parsed confidently",
        details={"after_state": observation_to_dict(after_observation)},
    )
