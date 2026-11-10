import { describe, expect, it } from 'vitest'
import { barHeight, dayLabel } from './change-trend'

describe('change trend', () => {
  it('names days as written, not through a time zone', () => {
    expect(dayLabel('2026-11-11', true)).toBe('11月11日')
    expect(dayLabel('2026-01-02', false)).toBe('Jan 2')
  })

  it('keeps any count above zero visible and leaves zero empty', () => {
    expect(barHeight(0, 5)).toBe('0')
    expect(barHeight(1, 0)).toBe('0')
    expect(barHeight(5, 5)).toBe('max(3px, 100%)')
    expect(barHeight(1, 40)).toBe('max(3px, 3%)')
  })
})
