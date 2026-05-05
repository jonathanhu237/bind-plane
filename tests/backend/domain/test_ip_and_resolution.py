from ipaddress import IPv4Address

import pytest
from bind_plane.db.models import CommandProfile, Credential, Network, Switch
from bind_plane.domain.ip import IPv4TargetError, parse_single_ipv4
from bind_plane.services.switch_resolution import (
    AmbiguousSwitchMatchError,
    NoSwitchMatchError,
    resolve_switch_for_ip,
)
from sqlalchemy.ext.asyncio import AsyncSession, async_sessionmaker


async def _create_switch(
    session,
    *,
    name: str,
    management_ip: str,
    cidr: str,
    validated: bool = True,
    enabled: bool = True,
) -> Switch:
    credential = Credential(
        name=f"{name}-credential",
        username="netops",
        encrypted_password="encrypted",
    )
    profile = CommandProfile(
        name=f"{name}-profile",
        command_templates={},
        prompt_patterns={},
        parser_rules={},
    )
    session.add_all([credential, profile])
    await session.flush()
    switch = Switch(
        name=name,
        management_ip=management_ip,
        credential_id=credential.id,
        command_profile_id=profile.id,
        is_enabled=enabled,
    )
    session.add(switch)
    await session.flush()
    session.add(
        Network(
            switch_id=switch.id,
            cidr=cidr,
            prefix_length=int(cidr.rsplit("/", 1)[1]),
            is_validated=validated,
            is_enabled=enabled,
        )
    )
    await session.flush()
    return switch


def test_parse_single_ipv4_rejects_invalid_ipv6_and_multiple_targets() -> None:
    assert parse_single_ipv4("10.44.132.254") == IPv4Address("10.44.132.254")

    for value in ["not-an-ip", "2001:db8::1", "10.0.0.1 10.0.0.2", "10.0.0.1,10.0.0.2"]:
        with pytest.raises(IPv4TargetError):
            parse_single_ipv4(value)


async def test_resolve_switch_uses_longest_enabled_validated_prefix(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        await _create_switch(session, name="broad", management_ip="10.0.0.1", cidr="10.44.0.0/16")
        specific = await _create_switch(
            session,
            name="specific",
            management_ip="10.0.0.2",
            cidr="10.44.132.0/24",
        )
        await _create_switch(
            session,
            name="disabled",
            management_ip="10.0.0.3",
            cidr="10.44.132.0/25",
            enabled=False,
        )
        await session.commit()

        resolved = await resolve_switch_for_ip(session, IPv4Address("10.44.132.254"))

    assert resolved.switch_id == specific.id
    assert resolved.cidr == "10.44.132.0/24"


async def test_resolve_switch_rejects_ambiguous_same_prefix(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        await _create_switch(session, name="left", management_ip="10.0.0.1", cidr="10.44.132.0/24")
        await _create_switch(session, name="right", management_ip="10.0.0.2", cidr="10.44.132.0/24")
        await session.commit()

        with pytest.raises(AmbiguousSwitchMatchError):
            await resolve_switch_for_ip(session, IPv4Address("10.44.132.100"))


async def test_resolve_switch_ignores_unvalidated_networks(
    session_factory: async_sessionmaker[AsyncSession],
) -> None:
    async with session_factory() as session:
        await _create_switch(
            session,
            name="draft",
            management_ip="10.0.0.1",
            cidr="10.44.132.0/24",
            validated=False,
        )
        await session.commit()

        with pytest.raises(NoSwitchMatchError):
            await resolve_switch_for_ip(session, IPv4Address("10.44.132.100"))
