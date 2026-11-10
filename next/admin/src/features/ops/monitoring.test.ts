import { describe, expect, it } from 'vitest'
import { formatBps, formatSpeed, formatUptime, portState, utilization } from './monitoring'

describe('monitoring figures', () => {
  it('reads rates in the units network people use', () => {
    expect(formatBps(null)).toBe('—')
    expect(formatBps(950)).toBe('950 bps')
    expect(formatBps(12_400_000)).toBe('12.4 Mbps')
    expect(formatBps(950_000_000)).toBe('950 Mbps')
    expect(formatBps(1_250_000_000)).toBe('1.3 Gbps')
  })

  it('names port speeds as switches print them', () => {
    expect(formatSpeed(100)).toBe('100M')
    expect(formatSpeed(1000)).toBe('1G')
    expect(formatSpeed(2500)).toBe('2.5G')
    expect(formatSpeed(null)).toBe('—')
  })

  it('gives uptime to its two largest units', () => {
    expect(formatUptime(3 * 86400 + 5 * 3600 + 59, true)).toBe('3 天 5 小时')
    expect(formatUptime(2 * 3600 + 7 * 60, false)).toBe('2h 7m')
    expect(formatUptime(20, true)).toBe('1 分钟')
    expect(formatUptime(null, false)).toBe('—')
  })

  it('measures a port by its busier direction', () => {
    expect(utilization({ in_bps: 100_000_000, out_bps: 250_000_000, speed_mbps: 1000 })).toBeCloseTo(25)
    expect(utilization({ in_bps: null, out_bps: 1, speed_mbps: 1000 })).toBeNull()
    expect(utilization({ in_bps: 1, out_bps: 1, speed_mbps: null })).toBeNull()
  })

  it('tells a port shut down apart from one with nothing plugged in', () => {
    expect(portState({ admin_up: true, oper_up: true })).toBe('up')
    expect(portState({ admin_up: true, oper_up: false })).toBe('down')
    expect(portState({ admin_up: false, oper_up: false })).toBe('off')
  })
})
