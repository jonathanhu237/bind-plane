import type { NetworkAsset, SwitchAsset } from './asset-editors'

/** An IPv4 address as a number, or null when the text is not one. */
export function ipv4(text: string): number | null {
  const parts = text.trim().split('.')
  if (parts.length !== 4 || parts.some((part) => !/^\d{1,3}$/.test(part) || Number(part) > 255)) return null
  return parts.reduce((value, part) => value * 256 + Number(part), 0)
}
export function prefixOf(cidr: string): { base: number; bits: number } | null {
  const [address, bits] = cidr.split('/')
  const base = ipv4(address)
  return base === null || !/^\d{1,2}$/.test(bits ?? '') || Number(bits) > 32 ? null : { base, bits: Number(bits) }
}
export function contains(cidr: string, address: number): boolean {
  const prefix = prefixOf(cidr)
  if (!prefix) return false
  const size = 2 ** (32 - prefix.bits)
  return Math.floor(address / size) === Math.floor(prefix.base / size)
}

export type NetworkRow = { network: NetworkAsset; owner?: SwitchAsset; bits: number }
export const usable = (row: NetworkRow) => row.network.is_enabled && row.network.is_validated && Boolean(row.owner?.is_enabled)

/**
 * Which network a release of the address would use, by the API's rule: the longest usable prefix,
 * and none when two switches tie for it.
 */
export function resolve(rows: NetworkRow[]): { kind: 'switch'; row: NetworkRow } | { kind: 'none' } | { kind: 'ambiguous'; rows: NetworkRow[] } {
  const candidates = rows.filter(usable)
  if (!candidates.length) return { kind: 'none' }
  const best = Math.max(...candidates.map((row) => row.bits))
  const top = candidates.filter((row) => row.bits === best)
  return new Set(top.map((row) => row.network.switch_id)).size > 1 ? { kind: 'ambiguous', rows: top } : { kind: 'switch', row: top[0] }
}
