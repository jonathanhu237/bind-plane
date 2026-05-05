from ipaddress import IPv4Address, ip_network
from uuid import UUID

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession
from sqlalchemy.orm import joinedload

from bind_plane.db.models import Network, Switch
from bind_plane.domain.release import ResolvedSwitch


class SwitchResolutionError(Exception):
    pass


class NoSwitchMatchError(SwitchResolutionError):
    pass


class AmbiguousSwitchMatchError(SwitchResolutionError):
    pass


class SelectedSwitchUnavailableError(SwitchResolutionError):
    pass


async def resolve_switch_for_ip(session: AsyncSession, target_ip: IPv4Address) -> ResolvedSwitch:
    result = await session.execute(
        select(Network, Switch)
        .join(Switch, Network.switch_id == Switch.id)
        .where(
            Network.is_enabled.is_(True),
            Network.is_validated.is_(True),
            Switch.is_enabled.is_(True),
        )
    )

    matches: list[tuple[Network, Switch]] = []
    for network, switch in result.all():
        parsed_network = ip_network(network.cidr, strict=False)
        if target_ip in parsed_network:
            matches.append((network, switch))

    if not matches:
        raise NoSwitchMatchError

    best_prefix = max(network.prefix_length for network, _ in matches)
    best_matches = [
        (network, switch) for network, switch in matches if network.prefix_length == best_prefix
    ]
    if len(best_matches) > 1:
        raise AmbiguousSwitchMatchError

    network, switch = best_matches[0]
    return ResolvedSwitch(
        switch_id=switch.id,
        network_id=network.id,
        command_profile_id=switch.command_profile_id,
        management_ip=switch.management_ip,
        name=switch.name,
        cidr=network.cidr,
        prefix_length=network.prefix_length,
    )


async def resolve_selected_switch(session: AsyncSession, switch_id: UUID) -> ResolvedSwitch:
    switch = await session.scalar(
        select(Switch)
        .options(joinedload(Switch.credential), joinedload(Switch.command_profile))
        .where(Switch.id == switch_id)
    )
    if switch is None:
        raise SelectedSwitchUnavailableError("Selected switch was not found")
    if not switch.is_enabled:
        raise SelectedSwitchUnavailableError("Selected switch is not enabled")
    if not switch.credential.is_active:
        raise SelectedSwitchUnavailableError("Selected switch credential is not active")
    if not switch.command_profile.is_active:
        raise SelectedSwitchUnavailableError("Selected switch command profile is not active")

    return ResolvedSwitch(
        switch_id=switch.id,
        network_id=None,
        command_profile_id=switch.command_profile_id,
        management_ip=switch.management_ip,
        name=switch.name,
        cidr=None,
        prefix_length=None,
    )
