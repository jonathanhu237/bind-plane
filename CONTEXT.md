# Bind Plane

Bind Plane is an operator tool for removing a single IPv4 static IP–MAC binding from the responsible switch.

## Language

**Release**:
Removal of the static binding for one IPv4 address. A dynamic ARP record afterward does not mean the release failed.
_Avoid_: ARP deletion, IP deletion

**Switch asset**:
A managed switch that may own one or more network ranges and has an assigned credential and command profile.
_Avoid_: Network, credential

**Network**:
An enabled, validated IPv4 range associated with a switch for automatic resolution.
_Avoid_: Switch

**Command profile**:
The device-specific vocabulary and output interpretation rules for querying and releasing a binding.
_Avoid_: Hard-coded switch command

**Pre-release query**:
An observed binding state gathered before the operator can confirm a release.
_Avoid_: Release

**Release job**:
A durable record of one proposed or attempted release, including its initiator, target, reason, switch, state and execution evidence.
_Avoid_: Mail task, operation history entry

**Forced release**:
An administrator-confirmed release after a pre-release query found no record.
_Avoid_: Bypass

**Needs manual confirmation**:
A release outcome whose final switch state cannot be determined reliably; it is neither a success nor proof of failure.
_Avoid_: Success, timeout as final result
