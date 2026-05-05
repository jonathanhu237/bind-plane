from ipaddress import IPv4Address, ip_address


class IPv4TargetError(ValueError):
    pass


def parse_single_ipv4(value: str) -> IPv4Address:
    if "," in value or " " in value.strip():
        raise IPv4TargetError("Exactly one IPv4 address is required")

    try:
        parsed = ip_address(value.strip())
    except ValueError as exc:
        raise IPv4TargetError("Invalid IP address") from exc

    if not isinstance(parsed, IPv4Address):
        raise IPv4TargetError("Only IPv4 addresses are supported")
    return parsed
