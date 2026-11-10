"""A small SNMP agent that answers like a Huawei access switch, for tests and the sandbox.

Never deploy it. It serves the system group, a few Gigabit ports (with a VLAN interface and NULL0
that monitoring must skip) and Huawei's entity table for CPU, memory and temperature, over SNMP
v2c and, given a user, v3 with SHA and AES. Counters grow with time at each port's traffic rate.
"""

import bisect
import math
import random
import time

from pysnmp.carrier.asyncio.dgram import udp
from pysnmp.entity import config, engine
from pysnmp.entity.rfc3413 import cmdrsp, context
from pysnmp.proto.api import v2c
from pysnmp.smi import instrum

HUAWEI_ENTITY = "1.3.6.1.4.1.2011.5.25.31.1.1.1.1"
CHASSIS, BOARD = 67108867, 67108873


class _Table(instrum.AbstractMibInstrumController):
    """Answers GET, GETNEXT and GETBULK from a fresh snapshot of the agent's values."""

    def __init__(self, values):
        self.values = values

    def _snapshot(self):
        items = sorted((v2c.ObjectIdentifier(oid), value) for oid, value in self.values().items())
        return items, [oid for oid, _ in items]

    def read_variables(self, *var_binds, **context):
        items, _ = self._snapshot()
        found = dict(items)
        return [
            (oid, found.get(v2c.ObjectIdentifier(oid), v2c.NoSuchInstance()))
            for oid, _ in var_binds
        ]

    def read_next_variables(self, *var_binds, **context):
        items, oids = self._snapshot()
        answers = []
        for oid, _ in var_binds:
            at = bisect.bisect_right(oids, v2c.ObjectIdentifier(oid))
            answers.append(items[at] if at < len(items) else (oid, v2c.EndOfMibView()))
        return answers


class FakeSnmpAgent:
    def __init__(
        self,
        name="FAKE-SW",
        community="public",
        v3_user=None,
        ports=4,
        cpu=18,
        memory=37,
        temperature=42,
        wander=False,
    ):
        self.name = name
        self.community = community
        self.v3_user = v3_user  # (user, SHA passphrase, AES passphrase)
        self.ports = ports
        self.cpu, self.memory, self.temperature = cpu, memory, temperature
        self.wander = wander
        self.booted = time.time()
        self.down = {ports}
        # Bytes per second in and out; port 1 is the busy uplink.
        self.traffic = {
            i: (1_250_000 if i == 1 else 12_500 * i, 625_000 if i == 1 else 6_250 * i)
            for i in range(1, ports + 1)
        }
        self.errors = {i: 0 for i in range(1, ports + 1)}
        self.engine = None

    def restart(self):
        self.booted = time.time()

    def _level(self, base, swing):
        if not self.wander:
            return base
        return max(
            0,
            round(
                base + swing * math.sin(time.time() / 900) + random.uniform(-swing / 4, swing / 4)
            ),
        )

    def values(self) -> dict[str, object]:
        up = time.time() - self.booted
        values: dict[str, object] = {
            "1.3.6.1.2.1.1.1.0": v2c.OctetString(
                "Huawei Versatile Routing Platform Software\r\n"
                "VRP (R) software, Version 5.170 (S5735 V200R019C10SPC500)"
            ),
            "1.3.6.1.2.1.1.2.0": v2c.ObjectIdentifier("1.3.6.1.4.1.2011.2.23.469"),
            "1.3.6.1.2.1.1.3.0": v2c.TimeTicks(int(up * 100) % 2**32),
            "1.3.6.1.2.1.1.5.0": v2c.OctetString(self.name),
        }
        # Only the board reports; the chassis entry gives 0, as on a real switch.
        for column, level in (
            (5, self._level(self.cpu, 10)),
            (7, self._level(self.memory, 2)),
            (11, self._level(self.temperature, 2)),
        ):
            values[f"{HUAWEI_ENTITY}.{column}.{CHASSIS}"] = v2c.Integer(0)
            values[f"{HUAWEI_ENTITY}.{column}.{BOARD}"] = v2c.Integer(level)
        interfaces = {i: (f"GigabitEthernet0/0/{i}", 6) for i in range(1, self.ports + 1)}
        interfaces[100], interfaces[200] = ("Vlanif10", 136), ("NULL0", 1)
        for index, (name, if_type) in interfaces.items():
            port = if_type == 6
            received, sent = (
                self.traffic.get(index, (0, 0)) if port and index not in self.down else (0, 0)
            )
            values.update(
                {
                    f"1.3.6.1.2.1.2.2.1.2.{index}": v2c.OctetString(name),
                    f"1.3.6.1.2.1.2.2.1.3.{index}": v2c.Integer(if_type),
                    f"1.3.6.1.2.1.2.2.1.5.{index}": v2c.Gauge32(1_000_000_000 if port else 0),
                    f"1.3.6.1.2.1.2.2.1.7.{index}": v2c.Integer(1),
                    f"1.3.6.1.2.1.2.2.1.8.{index}": v2c.Integer(2 if index in self.down else 1),
                    f"1.3.6.1.2.1.2.2.1.10.{index}": v2c.Counter32(int(received * up) % 2**32),
                    f"1.3.6.1.2.1.2.2.1.14.{index}": v2c.Counter32(self.errors.get(index, 0)),
                    f"1.3.6.1.2.1.2.2.1.16.{index}": v2c.Counter32(int(sent * up) % 2**32),
                    f"1.3.6.1.2.1.2.2.1.20.{index}": v2c.Counter32(0),
                    f"1.3.6.1.2.1.31.1.1.1.1.{index}": v2c.OctetString(name),
                    f"1.3.6.1.2.1.31.1.1.1.6.{index}": v2c.Counter64(int(received * up)),
                    f"1.3.6.1.2.1.31.1.1.1.10.{index}": v2c.Counter64(int(sent * up)),
                    f"1.3.6.1.2.1.31.1.1.1.15.{index}": v2c.Gauge32(1000 if port else 0),
                    # A description in GBK, as campus switches store Chinese ones.
                    f"1.3.6.1.2.1.31.1.1.1.18.{index}": v2c.OctetString(
                        "上联核心".encode("gbk") if index == 1 else b""
                    ),
                }
            )
        return values

    def start(self, host="0.0.0.0", port=161):
        """Starts answering on the running event loop."""
        self.engine = engine.SnmpEngine()
        config.add_transport(
            self.engine, udp.DOMAIN_NAME, udp.UdpTransport().open_server_mode((host, port))
        )
        config.add_v1_system(self.engine, "fake-area", self.community)
        config.add_vacm_user(self.engine, 2, "fake-area", "noAuthNoPriv", (1, 3, 6), ())
        if self.v3_user:
            user, auth, priv = self.v3_user
            config.add_v3_user(
                self.engine,
                user,
                config.USM_AUTH_HMAC96_SHA,
                auth,
                config.USM_PRIV_CFB128_AES,
                priv,
            )
            config.add_vacm_user(self.engine, 3, user, "authPriv", (1, 3, 6), ())
        snmp_context = context.SnmpContext(self.engine)
        snmp_context.unregister_context_name(v2c.OctetString(""))
        snmp_context.register_context_name(v2c.OctetString(""), _Table(self.values))
        cmdrsp.GetCommandResponder(self.engine, snmp_context)
        cmdrsp.NextCommandResponder(self.engine, snmp_context)
        cmdrsp.BulkCommandResponder(self.engine, snmp_context)
        self.engine.transport_dispatcher.job_started(1)

    def stop(self):
        if self.engine is not None:
            self.engine.transport_dispatcher.close_dispatcher()
            self.engine = None
