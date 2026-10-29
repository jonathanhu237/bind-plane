"""A minimal Huawei-style Telnet switch for end-to-end tests. Never deploy it.

It speaks just enough VRP for Netmiko's huawei_telnet driver: a login dialogue,
command echo, paging control, the running configuration, a static ARP query and
`undo arp static` in system view.
"""

import asyncio
import os

DEFAULT_CONFIG = """!Software Version V200R019C10SPC500
#
sysname FAKE-SW
#
vlan batch 10 20
#
aaa
 local-user netops password irreversible-cipher $1c$fake$hash
#
interface Vlanif10
 ip address 10.20.30.1 255.255.255.0
#
interface Vlanif20
 ip address 10.20.40.1 255.255.255.0
#
arp static 10.20.30.40 00e0-fc12-3456
#
user-interface vty 0 4
 authentication-mode aaa
#
return"""


class _Lines:
    """Reads Telnet lines, dropping option negotiation and treating CR LF / CR NUL as one end."""

    def __init__(self, reader):
        self.reader = reader
        self.after_cr = False

    async def byte(self) -> int:
        data = await self.reader.read(1)
        if not data:
            raise ConnectionError("client closed the session")
        return data[0]

    async def line(self) -> str:
        buffer = bytearray()
        while True:
            b = await self.byte()
            if b == 255:  # IAC
                command = await self.byte()
                if command in (251, 252, 253, 254):
                    await self.byte()
                elif command == 250:
                    while not (await self.byte() == 255 and await self.byte() == 240):
                        pass
                continue
            if self.after_cr and b in (0, 10):
                self.after_cr = False
                continue
            self.after_cr = b == 13
            if b in (10, 13):
                return buffer.decode("utf-8", errors="replace")
            buffer.append(b)


class FakeSwitch:
    def __init__(
        self, username, password, *, config_file=None, static_arp=None, hostname="FAKE-SW"
    ):
        self.username, self.password = username, password
        self.config_file = config_file
        self.hostname = hostname
        self.arp = {ip: [mac, "static"] for ip, mac in (static_arp or {}).items()}
        self.config_reads = 0

    def configuration(self) -> str:
        self.config_reads += 1
        if self.config_file and os.path.exists(self.config_file):
            with open(self.config_file, encoding="utf-8") as handle:
                return handle.read().rstrip("\n")
        return DEFAULT_CONFIG

    def execute(self, line: str, view: str) -> tuple[str, str, bool]:
        words = line.split()
        if not words:
            return "", view, False
        if line == "screen-length 0 temporary":
            return (
                "Info: The configuration takes effect on the current user terminal interface only.",
                view,
                False,
            )
        if line == "display current-configuration":
            return self.configuration(), view, False
        if words[:3] == ["display", "arp", "all"]:
            wanted = words[5] if len(words) == 6 and words[3:5] == ["|", "include"] else None
            rows = [
                f"{ip:<16}{mac:<16}{kind}"
                for ip, (mac, kind) in sorted(self.arp.items())
                if wanted in (None, ip)
            ]
            return (
                "\r\n".join(["IP ADDRESS      MAC ADDRESS     TYPE", *rows, f"Total:{len(rows)}"]),
                view,
                False,
            )
        if line == "system-view":
            return "Enter system view, return user view with return command.", "system", False
        if line == "return":
            return "", "user", False
        if line == "quit":
            return "", "user", view == "user"
        if view == "system" and words[:3] == ["undo", "arp", "static"] and len(words) == 4:
            entry = self.arp.get(words[3])
            if not entry or entry[1] != "static":
                return "Error: The static ARP entry does not exist.", view, False
            entry[1] = "dynamic"  # the host is still talking, so a dynamic entry is learned again
            return "", view, False
        return "Error: Unrecognized command found at '^' position.", view, False

    async def handle(self, reader, writer):
        lines = _Lines(reader)

        def send(text):
            writer.write(text.encode("utf-8"))

        try:
            send("\r\nLogin authentication\r\n\r\n\r\nUsername:")
            await writer.drain()
            username = await lines.line()
            send(username + "\r\nPassword:")
            await writer.drain()
            if (username, await lines.line()) != (self.username, self.password):
                send("\r\nError: Local authentication is rejected.\r\n")
                await writer.drain()
                return
            view = "user"
            send(f"\r\nInfo: The max number of VTY users is 5.\r\n\r\n<{self.hostname}>")
            await writer.drain()
            while True:
                line = (await lines.line()).strip()
                output, view, close = self.execute(line, view)
                prompt = f"<{self.hostname}>" if view == "user" else f"[{self.hostname}]"
                send(
                    line + "\r\n" + (output + "\r\n" if output else "") + ("" if close else prompt)
                )
                await writer.drain()
                if close:
                    return
        except (ConnectionError, OSError):
            pass
        finally:
            writer.close()


async def main():
    arp = dict(
        pair.split("=", 1)
        for pair in os.environ.get("FAKE_SWITCH_STATIC_ARP", "").split(",")
        if "=" in pair
    )
    switch = FakeSwitch(
        os.environ.get("FAKE_SWITCH_USERNAME", "netops"),
        os.environ["FAKE_SWITCH_PASSWORD"],
        config_file=os.environ.get("FAKE_SWITCH_CONFIG_FILE"),
        static_arp=arp,
    )
    server = await asyncio.start_server(
        switch.handle,
        os.environ.get("FAKE_SWITCH_HOST", "0.0.0.0"),
        int(os.environ.get("FAKE_SWITCH_PORT", "23")),
    )
    async with server:
        await server.serve_forever()


if __name__ == "__main__":
    asyncio.run(main())
