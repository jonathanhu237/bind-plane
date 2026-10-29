import { describe, expect, it } from 'vitest'
import { describeCron, taskState } from './api'

describe('device operation helpers', () => {
  it('reads common cron shapes in plain language and leaves the rest as written', () => {
    expect(describeCron('0 */4 * * *', false)).toBe('Every 4 hours at :00')
    expect(describeCron('*/10 * * * *', true)).toBe('每 10 分钟')
    expect(describeCron('30 2 * * *', false)).toBe('Daily at 02:30')
    expect(describeCron('0 3 * * 1', true)).toBe('每周一 03:00')
    expect(describeCron('0 3 1 * *', false)).toBe('0 3 1 * *')
    expect(describeCron('nonsense', false)).toBe('nonsense')
    expect(describeCron('* * * * *', true)).toBe('每分钟')
  })

  it('derives a run state from item counts', () => {
    const base = { total: 3, queued: 0, running: 0, succeeded: 3, failed: 0, skipped: 0 }
    expect(taskState(base)).toBe('succeeded')
    expect(taskState({ ...base, queued: 1, succeeded: 2 })).toBe('running')
    expect(taskState({ ...base, succeeded: 2, failed: 1 })).toBe('partial')
    expect(taskState({ ...base, succeeded: 0, failed: 3 })).toBe('failed')
    expect(taskState({ ...base, succeeded: 0, skipped: 3 })).toBe('empty')
    expect(taskState({ ...base, total: 0, succeeded: 0 })).toBe('empty')
  })
})
