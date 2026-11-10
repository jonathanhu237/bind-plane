import { describe, expect, it } from 'vitest'
import { attachmentName, describeCron, taskState } from './api'

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

describe('attachmentName', () => {
  it('reads the names Go gives downloads', () => {
    expect(attachmentName('attachment; filename=CORE-SW-01_10.0.0.1_20261111-1530.cfg')).toBe('CORE-SW-01_10.0.0.1_20261111-1530.cfg')
    expect(attachmentName('attachment; filename="core sw_10.0.0.1_20261111-1530.cfg"')).toBe('core sw_10.0.0.1_20261111-1530.cfg')
    expect(attachmentName("attachment; filename*=utf-8''%E6%A0%B8%E5%BF%83_10.0.0.1_20261111-1530.cfg")).toBe('核心_10.0.0.1_20261111-1530.cfg')
  })

  it('has no name without the header', () => {
    expect(attachmentName(null)).toBeNull()
    expect(attachmentName('inline')).toBeNull()
  })
})
