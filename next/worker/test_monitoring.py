"""The arithmetic of monitoring: readings, rates, restarts and threshold streaks."""

from datetime import UTC, datetime, timedelta

from pysnmp.proto.api import v2c
from worker.monitoring import (
    UPTIME_WRAP,
    Port,
    _text,
    has_restarted,
    new_errors,
    next_streak,
    rate,
    reading,
)

AT = datetime(2026, 11, 11, 12, 0, tzinfo=UTC)


def port(octets, bits=64, speed=1000, errors=(0, 0)):
    return Port(
        1, "GE0/0/1", None, 6, True, True, speed, octets, octets, errors[0], errors[1], bits
    )


def before(octets, seconds=300, bits=64, errors=(0, 0)):
    return {
        "in_octets": octets,
        "out_octets": octets,
        "in_errors": errors[0],
        "out_errors": errors[1],
        "counter_bits": bits,
        "sampled_at": AT - timedelta(seconds=seconds),
    }


def test_a_reading_is_the_highest_sane_value():
    # Entity tables give 0 for the chassis and fans, and some agents 65535 for "not supported".
    assert reading([v2c.Integer(0), v2c.Integer(23), v2c.Integer(0)], "cpu") == 23
    assert reading([v2c.Integer(0), v2c.Integer(0)], "cpu") == 0
    assert reading([v2c.Integer(0), v2c.Integer(65535)], "temperature") is None
    assert reading([v2c.Integer(0), v2c.Integer(47)], "temperature") == 47
    assert reading([v2c.Integer(130)], "memory") is None
    assert reading([], "cpu") is None


def test_rates_from_64_bit_counters():
    assert rate(before(1_000_000), port(1_000_000 + 375_000), "in_octets", AT, False) == 10_000
    assert rate(before(5_000), port(1_000), "in_octets", AT, False) is None  # reset
    assert rate(before(1_000), port(2_000), "in_octets", AT, True) is None  # restarted
    assert rate(None, port(2_000), "in_octets", AT, False) is None
    assert rate(before(1_000, bits=32), port(2_000), "in_octets", AT, False) is None


def test_rates_from_32_bit_counters_wrap_only_on_slow_ports():
    wrapped = rate(
        before(2**32 - 1000, bits=32), port(2000, bits=32, speed=100), "in_octets", AT, False
    )
    assert wrapped == round(3000 * 8 / 300)
    # At a gigabit a 32-bit counter can wrap more than once in five minutes, so it tells nothing.
    assert (
        rate(before(1000, bits=32), port(2000, bits=32, speed=1000), "in_octets", AT, False) is None
    )


def test_new_errors_since_the_previous_poll():
    assert new_errors(before(0, errors=(3, 4)), port(0, errors=(5, 9)), False) == 7
    assert new_errors(before(0, errors=(3, 4)), port(0, errors=(0, 0)), False) is None
    assert new_errors(None, port(0, errors=(1, 1)), False) is None


def test_a_restart_is_uptime_going_back_but_not_wrapping():
    assert has_restarted(86_400, AT - timedelta(minutes=5), 120, AT)
    assert not has_restarted(86_400, AT - timedelta(minutes=5), 86_700, AT)
    assert not has_restarted(None, None, 120, AT)
    # 497 days in, sysUpTime wraps to a small number without any restart.
    near_wrap = int(UPTIME_WRAP) - 100
    assert not has_restarted(near_wrap, AT - timedelta(minutes=5), 200, AT)


def test_streaks_count_polls_in_a_row_on_one_side_of_the_threshold():
    assert next_streak(0, 90, 80) == 1
    assert next_streak(2, 85, 80) == 3
    assert next_streak(3, 50, 80) == -1
    assert next_streak(-1, 50, 80) == -2
    assert next_streak(2, None, 80) == 2  # no reading keeps the count
    assert next_streak(5, 90, None) == 0  # no threshold, no count


def test_descriptions_in_gbk_and_utf8():
    assert _text(v2c.OctetString("上联核心".encode("gbk"))) == "上联核心"
    assert _text(v2c.OctetString("上联核心".encode())) == "上联核心"
    assert _text(v2c.OctetString(b"")) is None
