"""Device monitoring: polls switches over SNMP for uptime, CPU, memory, temperature and ports.

Polling only reads. A switch is polled while it is enabled and has an active SNMP credential;
each poll is due `interval_minutes` after the previous one. Standard MIBs give the system and the
ports; the command profile's `snmp_rules` say where the vendor keeps CPU, memory and temperature:

    {"cpu": ["<oid>", "<fallback oid>"], "memory": [...], "temperature": [...]}

Each OID is walked, scalar or table column alike, and the highest sane value of the first OID that
gives one is the reading: entity tables report 0 for chassis, fans and ports, and only the boards
carry real values.
"""

import asyncio
import base64
import json
import logging
import os
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime, timedelta
from decimal import Decimal

from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from pysnmp.hlapi.v3arch.asyncio import (
    CommunityData,
    ContextData,
    ObjectIdentity,
    ObjectType,
    SnmpEngine,
    UdpTransportTarget,
    UsmUserData,
    bulk_walk_cmd,
    get_cmd,
    usmAesCfb128Protocol,
    usmAesCfb192Protocol,
    usmAesCfb256Protocol,
    usmDESPrivProtocol,
    usmHMAC128SHA224AuthProtocol,
    usmHMAC192SHA256AuthProtocol,
    usmHMAC256SHA384AuthProtocol,
    usmHMAC384SHA512AuthProtocol,
    usmHMACMD5AuthProtocol,
    usmHMACSHAAuthProtocol,
    usmNoAuthProtocol,
    usmNoPrivProtocol,
)
from pysnmp.proto.rfc1905 import EndOfMibView, NoSuchInstance, NoSuchObject

logger = logging.getLogger("bind_plane.worker.monitoring")

SYS_DESCR = "1.3.6.1.2.1.1.1.0"
SYS_OBJECT_ID = "1.3.6.1.2.1.1.2.0"
SYS_UPTIME = "1.3.6.1.2.1.1.3.0"
SYS_NAME = "1.3.6.1.2.1.1.5.0"
# IF-MIB columns, by what they hold. The 64-bit counters and ifHighSpeed (in Mbps) come from
# ifXTable; the 32-bit ones are the fallback for switches without it.
IF_COLUMNS = {
    "descr": "1.3.6.1.2.1.2.2.1.2",
    "type": "1.3.6.1.2.1.2.2.1.3",
    "speed": "1.3.6.1.2.1.2.2.1.5",
    "admin": "1.3.6.1.2.1.2.2.1.7",
    "oper": "1.3.6.1.2.1.2.2.1.8",
    "in_octets": "1.3.6.1.2.1.2.2.1.10",
    "in_errors": "1.3.6.1.2.1.2.2.1.14",
    "out_octets": "1.3.6.1.2.1.2.2.1.16",
    "out_errors": "1.3.6.1.2.1.2.2.1.20",
    "name": "1.3.6.1.2.1.31.1.1.1.1",
    "hc_in_octets": "1.3.6.1.2.1.31.1.1.1.6",
    "hc_out_octets": "1.3.6.1.2.1.31.1.1.1.10",
    "high_speed": "1.3.6.1.2.1.31.1.1.1.15",
    "alias": "1.3.6.1.2.1.31.1.1.1.18",
}
# Ethernet of every speed, and link aggregation; VLAN interfaces, loopbacks and the rest carry no
# cables to watch.
PORT_TYPES = {6, 62, 69, 117, 161}
METRICS = ("cpu", "memory", "temperature")
SANE = {"cpu": (0, 100), "memory": (0, 100), "temperature": (1, 150)}
AUTH_PROTOCOLS = {
    "md5": usmHMACMD5AuthProtocol,
    "sha": usmHMACSHAAuthProtocol,
    "sha224": usmHMAC128SHA224AuthProtocol,
    "sha256": usmHMAC192SHA256AuthProtocol,
    "sha384": usmHMAC256SHA384AuthProtocol,
    "sha512": usmHMAC384SHA512AuthProtocol,
}
PRIV_PROTOCOLS = {
    "des": usmDESPrivProtocol,
    "aes": usmAesCfb128Protocol,
    "aes192": usmAesCfb192Protocol,
    "aes256": usmAesCfb256Protocol,
}
DEFAULT_PORT = 161
TIMEOUT = 2.0
RETRIES = 1
MAX_REPETITIONS = 20
LEASE = timedelta(minutes=3)
# sysUpTime counts hundredths of a second in 32 bits, so it wraps after about 497 days.
UPTIME_WRAP = 2**32 / 100
RESTART_ALERT_HOURS = 24
ALERT_KINDS = {"cpu": "cpu_high", "memory": "memory_high", "temperature": "temperature_high"}
ALERT_TEXT = {
    "cpu": "CPU at {value}% for {polls} polls (threshold {threshold}%)",
    "memory": "Memory at {value}% for {polls} polls (threshold {threshold}%)",
    "temperature": "Temperature at {value}°C for {polls} polls (threshold {threshold}°C)",
}


class SnmpFailure(Exception):
    """The switch did not answer, or refused the credential."""


def open_secret(value: str | None, key: bytes) -> str | None:
    """A secret sealed by the API (AES-GCM, as login credentials are)."""
    if not value:
        return None
    if not value.startswith("v1:"):
        raise ValueError("Unsupported credential encryption format")
    payload = base64.urlsafe_b64decode(value[3:] + "=" * (-len(value[3:]) % 4))
    return AESGCM(key).decrypt(payload[:12], payload[12:], None).decode("utf-8")


def snmp_auth(dep, key: bytes):
    if dep["snmp_version"] == "v2c":
        return CommunityData(open_secret(dep["encrypted_community"], key), mpModel=1)
    auth = dep["auth_protocol"]
    priv = dep["priv_protocol"]
    return UsmUserData(
        dep["snmp_username"],
        authKey=open_secret(dep["encrypted_auth_password"], key) if auth else None,
        privKey=open_secret(dep["encrypted_priv_password"], key) if priv else None,
        authProtocol=AUTH_PROTOCOLS[auth] if auth else usmNoAuthProtocol,
        privProtocol=PRIV_PROTOCOLS[priv] if priv else usmNoPrivProtocol,
    )


def _present(value) -> bool:
    return not isinstance(value, NoSuchObject | NoSuchInstance | EndOfMibView)


def _number(value) -> int | None:
    try:
        return int(value)
    except (TypeError, ValueError):
        return None


def _text(value) -> str | None:
    """An OCTET STRING as text. Campus switches write Chinese descriptions in GBK."""
    if value is None:
        return None
    raw = value.asOctets() if hasattr(value, "asOctets") else str(value).encode()
    raw = raw.rstrip(b"\x00")
    for encoding in ("utf-8", "gbk"):
        try:
            text = raw.decode(encoding)
            break
        except UnicodeDecodeError:
            continue
    else:
        text = raw.decode("latin-1")
    return text.strip() or None


class Session:
    """One switch's SNMP agent, reached with its credential."""

    def __init__(self, engine, auth, target):
        self.engine = engine
        self.auth = auth
        self.target = target

    @classmethod
    async def open(cls, engine, dep, key):
        target = await UdpTransportTarget.create(
            (dep["management_ip"], dep["snmp_port"] or DEFAULT_PORT),
            timeout=TIMEOUT,
            retries=RETRIES,
        )
        return cls(engine, snmp_auth(dep, key), target)

    async def get(self, *oids: str) -> dict[str, object]:
        error, status, _, binds = await get_cmd(
            self.engine,
            self.auth,
            self.target,
            ContextData(),
            *(ObjectType(ObjectIdentity(oid)) for oid in oids),
            lookupMib=False,
        )
        if error:
            raise SnmpFailure(str(error))
        if status:
            raise SnmpFailure(f"SNMP error: {status.prettyPrint()}")
        return {str(name): value for name, value in binds if _present(value)}

    async def walk(self, oid: str) -> dict[tuple[int, ...], object]:
        """A subtree as {index suffix: value}; empty when the agent has nothing there."""
        base = tuple(int(part) for part in oid.split("."))
        rows: dict[tuple[int, ...], object] = {}
        async for error, status, _, binds in bulk_walk_cmd(
            self.engine,
            self.auth,
            self.target,
            ContextData(),
            0,
            MAX_REPETITIONS,
            ObjectType(ObjectIdentity(oid)),
            lexicographicMode=False,
            lookupMib=False,
        ):
            if error:
                raise SnmpFailure(str(error))
            if status:
                raise SnmpFailure(f"SNMP error: {status.prettyPrint()}")
            for name, value in binds:
                suffix = tuple(name)[len(base) :]
                if tuple(name)[: len(base)] == base and _present(value):
                    rows[suffix] = value
        return rows


def reading(values, metric: str) -> int | None:
    """The highest sane value of a walked metric, or None when it gave none."""
    low, high = SANE[metric]
    numbers = [n for n in (_number(v) for v in values) if n is not None and low <= n <= high]
    return max(numbers) if numbers else None


async def read_metric(session: Session, oids, metric: str) -> int | None:
    for oid in oids if isinstance(oids, list) else []:
        if not isinstance(oid, str):
            continue
        value = reading((await session.walk(oid.strip("."))).values(), metric)
        if value is not None:
            return value
    return None


@dataclass
class Port:
    if_index: int
    name: str
    alias: str | None
    if_type: int
    admin_up: bool | None
    oper_up: bool | None
    speed_mbps: int | None
    in_octets: int | None
    out_octets: int | None
    in_errors: int | None
    out_errors: int | None
    counter_bits: int


async def read_ports(session: Session) -> list[Port]:
    columns: dict[str, dict[int, object]] = {}
    for key, oid in IF_COLUMNS.items():
        if key == "descr":
            continue
        columns[key] = {
            suffix[0]: value
            for suffix, value in (await session.walk(oid)).items()
            if len(suffix) == 1
        }
    if not columns["name"]:  # switches without ifXTable name ports in ifDescr
        columns["name"] = {
            s[0]: v for s, v in (await session.walk(IF_COLUMNS["descr"])).items() if len(s) == 1
        }
    ports = []
    for index in sorted(columns["type"]):
        if_type = _number(columns["type"][index])
        if if_type not in PORT_TYPES:
            continue
        status = {key: _number(columns[key].get(index)) for key in ("admin", "oper")}
        wide = index in columns["hc_in_octets"] and index in columns["hc_out_octets"]
        octets = ("hc_in_octets", "hc_out_octets") if wide else ("in_octets", "out_octets")
        high_speed = _number(columns["high_speed"].get(index))
        speed = (
            high_speed if high_speed else (_number(columns["speed"].get(index)) or 0) // 1_000_000
        )
        ports.append(
            Port(
                if_index=index,
                name=_text(columns["name"].get(index)) or f"ifIndex {index}",
                alias=_text(columns["alias"].get(index)),
                if_type=if_type,
                admin_up=None if status["admin"] is None else status["admin"] == 1,
                oper_up=None if status["oper"] is None else status["oper"] == 1,
                speed_mbps=speed or None,
                in_octets=_number(columns[octets[0]].get(index)),
                out_octets=_number(columns[octets[1]].get(index)),
                in_errors=_number(columns["in_errors"].get(index)),
                out_errors=_number(columns["out_errors"].get(index)),
                counter_bits=64 if wide else 32,
            )
        )
    return ports


@dataclass
class Poll:
    response_ms: int
    sys_name: str | None
    sys_descr: str | None
    sys_object_id: str | None
    uptime_seconds: int | None
    metrics: dict[str, int | None]
    ports: list[Port] = field(default_factory=list)


async def poll_switch(engine, dep, key) -> Poll:
    session = await Session.open(engine, dep, key)
    started = time.monotonic()
    system = await session.get(SYS_DESCR, SYS_OBJECT_ID, SYS_UPTIME, SYS_NAME)
    response_ms = round((time.monotonic() - started) * 1000)
    rules = dep["snmp_rules"]
    rules = json.loads(rules) if isinstance(rules, str) else rules or {}
    metrics = {metric: await read_metric(session, rules.get(metric), metric) for metric in METRICS}
    uptime = _number(system.get(SYS_UPTIME))
    return Poll(
        response_ms=response_ms,
        sys_name=_text(system.get(SYS_NAME)),
        sys_descr=_text(system.get(SYS_DESCR)),
        sys_object_id=str(system[SYS_OBJECT_ID]) if SYS_OBJECT_ID in system else None,
        uptime_seconds=None if uptime is None else uptime // 100,
        metrics=metrics,
        ports=await read_ports(session),
    )


def describe_failure(exc: BaseException) -> str:
    """A message safe to store and show; SNMP errors never carry the community or passwords."""
    if isinstance(exc, SnmpFailure):
        return str(exc)
    if isinstance(exc, TimeoutError):
        return "Timed out"
    if isinstance(exc, OSError):
        return f"Network error ({exc.__class__.__name__})"
    return f"Unexpected error ({exc.__class__.__name__})"


def rate(previous: dict | None, port: Port, key: str, at: datetime, restarted: bool) -> int | None:
    """Bits per second since the previous poll, or None when the counters cannot tell.

    A 64-bit counter that went down was reset. A 32-bit one wraps, which is only told apart from
    a reset while the port is too slow to wrap twice between polls.
    """
    octets = getattr(port, key)
    if previous is None or restarted or octets is None or previous[key] is None:
        return None
    if previous["counter_bits"] != port.counter_bits:
        return None
    seconds = (at - previous["sampled_at"]).total_seconds()
    if seconds <= 0:
        return None
    delta = octets - int(previous[key])
    if port.counter_bits == 32:
        if port.speed_mbps and port.speed_mbps * 1_000_000 / 8 * seconds >= 2**32:
            return None
        if delta < 0:
            delta += 2**32
    if delta < 0:
        return None
    return round(delta * 8 / seconds)


def new_errors(previous: dict | None, port: Port, restarted: bool) -> int | None:
    if previous is None or restarted or None in (port.in_errors, port.out_errors):
        return None
    if previous["in_errors"] is None or previous["out_errors"] is None:
        return None
    delta = port.in_errors + port.out_errors - previous["in_errors"] - previous["out_errors"]
    return delta if delta >= 0 else None


def has_restarted(previous_uptime, previous_at, uptime, at) -> bool:
    """Whether the uptime went back since the previous poll, other than by wrapping around."""
    if (
        previous_uptime is None
        or previous_at is None
        or uptime is None
        or uptime >= previous_uptime
    ):
        return False
    expected = previous_uptime + (at - previous_at).total_seconds()
    return not (expected > UPTIME_WRAP - 86400 and abs(uptime - (expected - UPTIME_WRAP)) < 3600)


def next_streak(previous: int, value: int | None, threshold: int | None) -> int:
    """Polls in a row at or above the threshold (positive) or below it (negative)."""
    if threshold is None:
        return 0
    if value is None:
        return previous
    if value >= threshold:
        return previous + 1 if previous > 0 else 1
    return previous - 1 if previous < 0 else -1


async def upsert_alert(conn, kind, switch_id, message):
    await conn.execute(
        """INSERT INTO bp_alerts(kind,severity,switch_id,message,dedupe_key)
        VALUES ($1,'warning',$2::uuid,$3,$1||':'||$2::uuid::text)
        ON CONFLICT (dedupe_key) WHERE status<>'resolved'
        DO UPDATE SET last_seen_at=now(),message=EXCLUDED.message""",
        kind,
        switch_id,
        message,
    )


async def resolve_alert(conn, kind, switch_id):
    await conn.execute(
        """UPDATE bp_alerts SET status='resolved',resolved_at=now()
        WHERE dedupe_key=$1||':'||$2::uuid::text AND status<>'resolved'""",
        kind,
        switch_id,
    )


async def claim_polls(pool, limit: int):
    """Leases up to `limit` due switches; one never polled has no row yet and is due at once."""
    async with pool.acquire() as conn:
        return await conn.fetch(
            """WITH due AS (
                SELECT s.id FROM bp_switches s LEFT JOIN bp_switch_monitoring m ON m.switch_id=s.id
                WHERE s.is_enabled AND EXISTS (SELECT 1 FROM bp_snmp_credentials c
                    WHERE c.id=s.snmp_credential_id AND c.is_active)
                AND (m.switch_id IS NULL OR m.next_poll_at<=now()
                     AND (m.lease_until IS NULL OR m.lease_until<now()))
                ORDER BY m.next_poll_at NULLS FIRST LIMIT $1)
            INSERT INTO bp_switch_monitoring(switch_id,lease_token,lease_until)
            SELECT id,gen_random_uuid(),now()+$2::interval FROM due
            ON CONFLICT (switch_id) DO UPDATE
                SET lease_token=EXCLUDED.lease_token,lease_until=EXCLUDED.lease_until
                WHERE bp_switch_monitoring.next_poll_at<=now()
                AND (bp_switch_monitoring.lease_until IS NULL
                     OR bp_switch_monitoring.lease_until<now())
            RETURNING switch_id,lease_token""",
            limit,
            LEASE,
        )


async def load_target(pool, switch_id):
    async with pool.acquire() as conn:
        dep = await conn.fetchrow(
            """SELECT s.id,host(s.management_ip) AS management_ip,s.snmp_port,c.snmp_version,
            c.encrypted_community,c.username AS snmp_username,c.auth_protocol,
            c.encrypted_auth_password,c.priv_protocol,c.encrypted_priv_password,p.snmp_rules,
            m.uptime_seconds,m.succeeded_at,m.failures,m.streaks,m.restarted_at
            FROM bp_switches s JOIN bp_snmp_credentials c ON c.id=s.snmp_credential_id
            JOIN bp_command_profiles p ON p.id=s.command_profile_id
            JOIN bp_switch_monitoring m ON m.switch_id=s.id WHERE s.id=$1""",
            switch_id,
        )
        ports = await conn.fetch(
            """SELECT if_index,in_octets,out_octets,in_errors,out_errors,oper_up,oper_changed_at,
            counter_bits,sampled_at FROM bp_switch_interfaces WHERE switch_id=$1""",
            switch_id,
        )
        settings = await conn.fetchrow("SELECT * FROM bp_monitoring_settings")
    return dep, {row["if_index"]: dict(row) for row in ports}, settings


async def record_failure(conn, claim, dep, settings, message):
    failures = dep["failures"] + 1
    updated = await conn.execute(
        """UPDATE bp_switch_monitoring SET polled_at=now(),error=$3,failures=$4,
        next_poll_at=now()+make_interval(mins=>$5),lease_token=NULL,lease_until=NULL
        WHERE switch_id=$1 AND lease_token=$2""",
        claim["switch_id"],
        claim["lease_token"],
        message,
        failures,
        settings["interval_minutes"],
    )
    if updated != "UPDATE 1":
        return
    if failures >= settings["consecutive_polls"]:
        await upsert_alert(conn, "snmp_unreachable", dep["id"], f"SNMP not responding: {message}")


async def record_poll(conn, claim, dep, previous_ports, settings, poll: Poll, at: datetime):
    restarted = has_restarted(dep["uptime_seconds"], dep["succeeded_at"], poll.uptime_seconds, at)
    restarted_at = dep["restarted_at"]
    if restarted:
        restarted_at = at - timedelta(seconds=poll.uptime_seconds)
    streaks = dep["streaks"]
    streaks = json.loads(streaks) if isinstance(streaks, str) else dict(streaks or {})
    thresholds = {metric: settings[f"{metric}_threshold"] for metric in METRICS}
    for metric in METRICS:
        streaks[metric] = next_streak(
            int(streaks.get(metric, 0)), poll.metrics[metric], thresholds[metric]
        )
    updated = await conn.execute(
        """UPDATE bp_switch_monitoring SET polled_at=$3,succeeded_at=$3,error=NULL,failures=0,
        response_ms=$4,sys_name=$5,sys_descr=$6,sys_object_id=$7,uptime_seconds=$8,restarted_at=$9,
        cpu_percent=$10,memory_percent=$11,temperature_c=$12,streaks=$13::jsonb,
        next_poll_at=$3::timestamptz+make_interval(mins=>$14),lease_token=NULL,lease_until=NULL
        WHERE switch_id=$1 AND lease_token=$2""",
        claim["switch_id"],
        claim["lease_token"],
        at,
        poll.response_ms,
        poll.sys_name,
        poll.sys_descr,
        poll.sys_object_id,
        poll.uptime_seconds,
        restarted_at,
        poll.metrics["cpu"],
        poll.metrics["memory"],
        poll.metrics["temperature"],
        json.dumps(streaks),
        settings["interval_minutes"],
    )
    if updated != "UPDATE 1":
        return  # the switch stopped being monitored while it was polled
    switch_id = dep["id"]
    await conn.execute(
        """INSERT INTO bp_switch_metrics(switch_id,sampled_at,cpu_percent,memory_percent,
        temperature_c,response_ms) VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING""",
        switch_id,
        at,
        poll.metrics["cpu"],
        poll.metrics["memory"],
        poll.metrics["temperature"],
        poll.response_ms,
    )
    rows = []
    for port in poll.ports:
        previous = previous_ports.get(port.if_index)
        changed_at = previous["oper_changed_at"] if previous else None
        if previous and previous["oper_up"] is not None and previous["oper_up"] != port.oper_up:
            changed_at = at
        rows.append(
            (
                port.if_index,
                port.name,
                port.alias,
                port.if_type,
                port.admin_up,
                port.oper_up,
                port.speed_mbps,
                rate(previous, port, "in_octets", at, restarted),
                rate(previous, port, "out_octets", at, restarted),
                port.in_errors,
                port.out_errors,
                new_errors(previous, port, restarted),
                None if port.in_octets is None else Decimal(port.in_octets),
                None if port.out_octets is None else Decimal(port.out_octets),
                port.counter_bits,
                changed_at,
            )
        )
    await conn.execute(
        "DELETE FROM bp_switch_interfaces WHERE switch_id=$1 AND NOT (if_index=ANY($2::int[]))",
        switch_id,
        [port.if_index for port in poll.ports],
    )
    await conn.executemany(
        """INSERT INTO bp_switch_interfaces(switch_id,if_index,name,alias,if_type,admin_up,oper_up,
        speed_mbps,in_bps,out_bps,in_errors,out_errors,new_errors,in_octets,out_octets,
        counter_bits,oper_changed_at,sampled_at)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
        ON CONFLICT (switch_id,if_index) DO UPDATE SET name=EXCLUDED.name,alias=EXCLUDED.alias,
        if_type=EXCLUDED.if_type,admin_up=EXCLUDED.admin_up,oper_up=EXCLUDED.oper_up,
        speed_mbps=EXCLUDED.speed_mbps,in_bps=EXCLUDED.in_bps,out_bps=EXCLUDED.out_bps,
        in_errors=EXCLUDED.in_errors,out_errors=EXCLUDED.out_errors,new_errors=EXCLUDED.new_errors,
        in_octets=EXCLUDED.in_octets,out_octets=EXCLUDED.out_octets,
        counter_bits=EXCLUDED.counter_bits,oper_changed_at=EXCLUDED.oper_changed_at,
        sampled_at=EXCLUDED.sampled_at""",
        [(switch_id, *row, at) for row in rows],
    )

    # Alerts: SNMP answers again; a metric opens one after `consecutive_polls` polls in a row at or
    # above its threshold and resolves after as many below; a restart stays an alert for a day.
    await resolve_alert(conn, "snmp_unreachable", switch_id)
    needed = settings["consecutive_polls"]
    for metric in METRICS:
        kind, streak, threshold = ALERT_KINDS[metric], streaks[metric], thresholds[metric]
        if threshold is None or streak <= -needed:
            await resolve_alert(conn, kind, switch_id)
        elif streak >= needed:
            text = ALERT_TEXT[metric].format(
                value=poll.metrics[metric], polls=streak, threshold=threshold
            )
            await upsert_alert(conn, kind, switch_id, text)
    if restarted and settings["alert_on_restart"]:
        await upsert_alert(conn, "device_restarted", switch_id, "Switch restarted")
    elif (
        not settings["alert_on_restart"]
        or restarted_at is None
        or at - restarted_at > timedelta(hours=RESTART_ALERT_HOURS)
    ):
        await resolve_alert(conn, "device_restarted", switch_id)


async def poll_and_record(pool, engine, claim, key):
    """Polls one leased switch and stores what it found; every failure is stored, never raised."""
    try:
        dep, previous_ports, settings = await load_target(pool, claim["switch_id"])
        if dep is None:
            return
        try:
            poll = await poll_switch(engine, dep, key)
        except Exception as exc:
            if not isinstance(exc, SnmpFailure | OSError | TimeoutError):
                logger.exception("monitoring poll of switch %s failed", claim["switch_id"])
            async with pool.acquire() as conn, conn.transaction():
                await record_failure(conn, claim, dep, settings, describe_failure(exc))
            return
        at = datetime.now(UTC)
        async with pool.acquire() as conn, conn.transaction():
            await record_poll(conn, claim, dep, previous_ports, settings, poll, at)
    except Exception:
        logger.exception("could not record the poll of switch %s", claim["switch_id"])


def credential_key() -> bytes:
    encoded = os.environ["BIND_PLANE_CREDENTIAL_ENCRYPTION_KEY"]
    return base64.urlsafe_b64decode(encoded + "=" * (-len(encoded) % 4))


async def monitor_loop(pool, concurrency: int):
    """Polls up to `concurrency` switches at once, independently of releases and device tasks."""
    key = credential_key()
    engine = SnmpEngine()
    running: set[asyncio.Task] = set()
    while True:
        try:
            free = concurrency - len(running)
            for claim in await claim_polls(pool, free) if free > 0 else []:
                task = asyncio.create_task(poll_and_record(pool, engine, claim, key))
                running.add(task)
                task.add_done_callback(running.discard)
        except Exception:
            logger.exception("monitoring queue unavailable")
            await asyncio.sleep(5)
            continue
        if running:
            await asyncio.wait(running, timeout=2, return_when=asyncio.FIRST_COMPLETED)
        else:
            await asyncio.sleep(2)
