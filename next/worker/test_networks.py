"""Networks read from configuration, in each campus dialect's shape (addresses replaced)."""

import pytest
from worker.devices import NetworksKept, network_plan
from worker.networks import IncompleteConfiguration, InterfaceNetwork, configuration_networks

H3C = """ sysname E-LAB-H3C
#
interface NULL0
#
interface LoopBack0
 ip address 192.0.2.254 255.255.255.255
#
interface Vlan-interface4
 description manage
 ip address 10.44.106.62 255.255.255.192
#
interface Vlan-interface40
 description Lab 3F
 ip address 10.20.40.1 255.255.255.0
 ip address 10.20.41.1 255.255.255.0 sub
#
interface Vlan-interface1242
 shutdown
 ip address 10.20.42.1 255.255.255.224
#
interface M-GigabitEthernet0/0/0
 ip address 192.168.0.1 255.255.255.0
#
interface GigabitEthernet1/0/49
 port link-mode route
 ip address 172.16.0.1 255.255.255.252
#
interface Ten-GigabitEthernet1/0/50
 port link-mode route
 ip address 172.16.0.5 30
#
interface Vlan-interface99
 undo shutdown
 ip address 10.20.99.1 24
#
return"""

HUAWEI = """#
 sysname E-LAB-HW
#
interface Vlanif20
 description dorm
 ip address 10.30.20.1 255.255.254.0
 ip address 10.30.22.1 255.255.255.0 sub
#
interface MEth0/0/1
 ip address 192.168.1.1 255.255.255.0
#
interface LoopBack1
 ip address 192.0.2.1 255.255.255.255
#
interface Vlanif30
 ip address 10.30.30.1 255.255.255.254
#
return"""

# Ruijie closes blocks with "!"; this capture also kept a pager prompt erased by backspaces.
PAGER = "\x08" * 8 + " " * 8 + "\x08" * 8
RUIJIE = f"""hostname E-LAB-RJ
!
interface VLAN 2
 ip address 10.40.2.1 255.255.255.192
 ip address 10.40.3.1 255.255.255.192 secondary
!
interface VLAN 506
{PAGER} ip address 10.40.6.1 255.255.255.0
!
interface Mgmt 0
 ip address 192.168.2.1 255.255.255.0
!
end"""


def cidrs(text):
    return [network.cidr for network in configuration_networks(text)]


def test_h3c_interfaces_with_users_become_networks():
    networks = configuration_networks(H3C)
    assert [n.cidr for n in networks] == [
        "10.20.40.0/24",
        "10.20.41.0/24",
        "10.20.99.0/24",
        "10.44.106.0/26",
        "172.16.0.0/30",
        "172.16.0.4/30",
    ]
    lab = networks[0]
    assert (lab.interface, lab.vlan, lab.description) == ("Vlan-interface40", "40", "Lab 3F")
    # A secondary address shares its interface's VLAN and description.
    assert networks[1] == InterfaceNetwork("10.20.41.0/24", "Vlan-interface40", "40", "Lab 3F")
    assert networks[4].vlan is None


def test_huawei_and_ruijie_dialects():
    assert cidrs(HUAWEI) == ["10.30.20.0/23", "10.30.22.0/24"]
    networks = configuration_networks(RUIJIE)
    assert [n.cidr for n in networks] == ["10.40.2.0/26", "10.40.3.0/26", "10.40.6.0/24"]
    assert [n.vlan for n in networks] == ["2", "2", "506"]


def test_a_network_on_two_interfaces_is_listed_once():
    text = H3C.replace("10.20.99.1 24", "10.20.40.2 255.255.255.0")
    assert cidrs(text).count("10.20.40.0/24") == 1


@pytest.mark.parametrize(
    "text",
    [
        H3C.rsplit("return", 1)[0],  # the session ended early
        "% User doesn't have sufficient privilege to execute this command.",
        "",
    ],
)
def test_a_configuration_that_does_not_end_is_refused(text):
    with pytest.raises(IncompleteConfiguration):
        configuration_networks(text)


def found(*cidrs_):
    return [InterfaceNetwork(cidr, "Vlan-interface1", "1", None) for cidr in cidrs_]


def stored(*rows):
    return [{"cidr": cidr, "interface": interface} for cidr, interface in rows]


def test_the_first_read_replaces_typed_in_networks():
    added, removed, kept = network_plan(
        stored(("10.0.1.0/24", None), ("10.0.2.0/24", None), ("10.0.3.0/24", None)),
        found("10.0.1.0/24", "10.0.9.0/24"),
        requested=False,
    )
    assert [n.cidr for n in added] == ["10.0.9.0/24"]
    assert removed == ["10.0.2.0/24", "10.0.3.0/24"]
    assert [n.cidr for n in kept] == ["10.0.1.0/24"]


def test_a_read_that_finds_nothing_keeps_every_network():
    with pytest.raises(NetworksKept, match="No interface addresses"):
        network_plan(stored(("10.0.1.0/24", "Vlan-interface1")), [], requested=True)
    assert network_plan([], [], requested=False) == ([], [], [])


def test_losing_most_networks_needs_someone_to_ask():
    before = stored(*[(f"10.0.{i}.0/24", "Vlan-interface1") for i in range(1, 5)])
    with pytest.raises(NetworksKept, match="remove 3 of 4 networks"):
        network_plan(before, found("10.0.1.0/24"), requested=False)
    _, removed, _ = network_plan(before, found("10.0.1.0/24"), requested=True)
    assert len(removed) == 3
    # Half, or fewer than three, is an ordinary change.
    _, removed, _ = network_plan(before, found("10.0.1.0/24", "10.0.2.0/24"), requested=False)
    assert removed == ["10.0.3.0/24", "10.0.4.0/24"]
