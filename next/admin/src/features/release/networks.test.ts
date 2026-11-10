import { describe, expect, it } from 'vitest'
import type { NetworkAsset, SwitchAsset } from './asset-editors'
import { contains, ipv4, resolve, type NetworkRow } from './networks'

const owner = (id: string, enabled = true) => ({ id, name: id, management_ip: '192.0.2.1', is_enabled: enabled }) as SwitchAsset
const row = (cidr: string, switchId: string, fields: Partial<NetworkAsset> = {}, enabled = true): NetworkRow => ({
  network: { id: `${switchId}-${cidr}`, switch_id: switchId, cidr, is_enabled: true, is_validated: true, vlan: null, description: null, version: 1, ...fields },
  owner: owner(switchId, enabled), bits: Number(cidr.split('/')[1]),
})

describe('network lookup', () => {
  it('reads IPv4 addresses and tests containment', () => {
    expect(ipv4('10.1.2.3')).toBe(167838211)
    expect(ipv4('10.1.2')).toBeNull()
    expect(ipv4('10.1.2.256')).toBeNull()
    expect(contains('10.1.2.0/24', ipv4('10.1.2.200')!)).toBe(true)
    expect(contains('10.1.2.0/24', ipv4('10.1.3.1')!)).toBe(false)
    expect(contains('10.1.2.3/32', ipv4('10.1.2.3')!)).toBe(true)
    expect(contains('0.0.0.0/0', ipv4('203.0.113.9')!)).toBe(true)
  })

  it('picks the most specific usable network, as releases do', () => {
    const result = resolve([row('10.0.0.0/8', 'core'), row('10.1.2.0/24', 'edge'), row('10.1.2.0/25', 'lab', { is_validated: false })])
    expect(result).toMatchObject({ kind: 'switch', row: { network: { switch_id: 'edge' } } })
  })

  it('stops on a tie between switches and when nothing usable is left', () => {
    expect(resolve([row('10.1.2.0/24', 'a'), row('10.1.2.0/24', 'b')])).toMatchObject({ kind: 'ambiguous' })
    expect(resolve([row('10.1.2.0/24', 'a', { is_enabled: false }), row('10.1.0.0/16', 'b', {}, false)])).toEqual({ kind: 'none' })
  })
})
