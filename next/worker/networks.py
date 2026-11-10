"""Networks read from a release switch's running configuration.

A switch that handles releases is the gateway of its users' networks, so each network is an
interface address in its configuration. Every supported dialect writes them alike:

    interface Vlan-interface40          (H3C; Vlanif40 on Huawei, VLAN 40 on Ruijie)
     description Lab 3F
     ip address 10.20.40.1 255.255.255.0
     ip address 10.20.41.1 255.255.255.0 sub      (secondary on Ruijie and Cisco)

An interface block is its `interface` line and the indented lines below it. Loopback, null
and management ports, shut-down interfaces and /31 or /32 addresses carry no users.
"""

import ipaddress
import re
from dataclasses import dataclass

INTERFACE = re.compile(r"^interface\s+(\S.*?)\s*$", re.IGNORECASE)
ADDRESS = re.compile(
    r"^\s+ip address\s+(\d{1,3}(?:\.\d{1,3}){3})\s+(\d{1,3}(?:\.\d{1,3}){3}|\d{1,2})"
    r"(?:\s+(?:sub|secondary))?\s*$",
    re.IGNORECASE,
)
DESCRIPTION = re.compile(r"^\s+description\s+(.+?)\s*$", re.IGNORECASE)
SHUTDOWN = re.compile(r"^\s+shutdown\s*$", re.IGNORECASE)
NO_USERS = re.compile(r"^(loopback|null|inloopback|m-?eth|m-gigabitethernet|mgmt)", re.IGNORECASE)
VLAN = re.compile(r"vlan(?:-interface|if)?\s*(\d+)$", re.IGNORECASE)
# Comware and VRP end a configuration with "return", RGOS and IOS with "end".
FINAL_LINES = ("return", "end")


@dataclass(frozen=True)
class InterfaceNetwork:
    cidr: str
    interface: str
    vlan: str | None
    description: str | None


class IncompleteConfiguration(ValueError):
    pass


ESCAPE = re.compile(r"\x1b\[[0-9;?]*[A-Za-z]")


def _without_pager(line: str) -> str:
    """The line as printed once a pager prompt ("--More--") was erased with backspaces.

    Paging is turned off before a read, but a switch that ignores that still erases its prompt
    by sending backspaces; the line's own text follows the last of them.
    """
    line = ESCAPE.sub("", line)
    return line[line.rfind("\x08") + 1 :] if "\x08" in line else line


def configuration_networks(text: str) -> list[InterfaceNetwork]:
    """Each network once, from the first interface that carries it, in CIDR order.

    Raises IncompleteConfiguration unless the text ends the way a whole configuration does,
    so a session cut short can never make networks look removed.
    """
    lines = [_without_pager(line) for line in text.replace("\r\n", "\n").split("\n")]
    final = next((line.strip().lower() for line in reversed(lines) if line.strip()), "")
    if final not in FINAL_LINES:
        raise IncompleteConfiguration("Configuration does not end with return or end")
    found: dict[str, InterfaceNetwork] = {}
    index = 0
    while index < len(lines):
        header = INTERFACE.match(lines[index])
        index += 1
        if not header:
            continue
        name = header.group(1)
        block = []
        while index < len(lines) and lines[index][:1].isspace():
            block.append(lines[index])
            index += 1
        if NO_USERS.match(name) or any(SHUTDOWN.match(line) for line in block):
            continue
        description = next((m.group(1) for m in map(DESCRIPTION.match, block) if m), None)
        vlan = VLAN.search(name)
        for line in block:
            address = ADDRESS.match(line)
            if not address:
                continue
            try:
                network = ipaddress.IPv4Network(
                    f"{address.group(1)}/{address.group(2)}", strict=False
                )
            except ValueError:  # a wildcard or malformed mask
                continue
            if network.prefixlen >= 31:
                continue
            found.setdefault(
                str(network),
                InterfaceNetwork(str(network), name, vlan.group(1) if vlan else None, description),
            )
    return sorted(found.values(), key=lambda n: ipaddress.IPv4Network(n.cidr))
