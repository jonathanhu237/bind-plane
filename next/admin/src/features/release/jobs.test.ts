import { describe, expect, it } from 'vitest'
import { canRelease, canRetry, entryLabel, jobsPath, phaseLabel, reasonLabel, releaseErrorText, statusLabel, stopText, targetIPProblem } from './jobs'
import type { User } from '@/shared/api/contracts'

describe('release job messages', () => {
  it('checks the typed address before asking a switch', () => {
    expect(targetIPProblem('', true)).toBe('请输入要解绑的 IPv4 地址。')
    expect(targetIPProblem('   ', false)).toBe('Enter the IPv4 address to release.')
    expect(targetIPProblem(' 10.20.30.41 ', true)).toBe('')
    expect(targetIPProblem('255.255.255.255', true)).toBe('')
    for (const value of ['10.20.30', '10.20.30.256', '10.20.30.041', '10.20.30.41/24', '2001:db8::1', 'core-sw']) {
      expect(targetIPProblem(value, true)).toBe('这不是有效的 IPv4 地址，请按 10.0.0.42 这样的格式输入。')
    }
  })

  it('explains refusals from the release API in the interface language', () => {
    expect(releaseErrorText(new Error('no enabled validated switch owns this IP'), true)).toBe('没有哪台已启用的交换机拥有包含该地址、且已验证的网段。管理员可以在“资产配置”中检查网段。')
    expect(releaseErrorText(new Error('switch configuration changed; try again'), true)).toBe('交换机配置刚刚有变更，请重新解绑。')
    expect(releaseErrorText(new Error('conflict'), true)).toBe('这个 IP 正在解绑，请等它完成后再试。')
    expect(releaseErrorText(new Error('operator role required'), true)).toBe('需要操作员或管理员角色。')
    expect(releaseErrorText(new Error('an unexpected detail'), true)).toBe('an unexpected detail')
    expect(releaseErrorText('not an error', true)).toBe('操作失败，请重试。')
    expect(stopText('stopped_ambiguous_switch', true)).toBe('有多台交换机的网段同样精确地包含该地址，需要管理员先修正网段数据。')
    expect(stopText('queued', true)).toBe('')
  })

  it('names job states, phases, bindings and reasons instead of showing their codes', () => {
    expect(statusLabel('stopped_no_switch', true)).toBe('未匹配到交换机')
    expect(statusLabel('succeeded', true)).toBe('已解绑')
    expect(statusLabel('no_static_binding', true)).toBe('无绑定')
    expect(statusLabel('no_static_binding', false)).toBe('Not bound')
    expect(statusLabel('no_record', true)).toBe('无记录')
    expect(statusLabel('no_record', false)).toBe('No record')
    expect(statusLabel('needs_manual_confirmation', true)).toBe('结果未知')
    expect(statusLabel('needs_manual_confirmation', false)).toBe('Outcome unknown')
    expect(statusLabel('some_new_state', true)).toBe('some new state')
    expect(phaseLabel('querying_after', true)).toBe('正在查询解绑后状态')
    expect(entryLabel('dynamic', true)).toBe('动态记录')
    expect(entryLabel(undefined, true)).toBe('—')
    expect(reasonLabel('temporary_test', true)).toBe('临时测试')
    expect(reasonLabel('temporary_test', false)).toBe('Temporary test')
  })
})

describe('release jobs', () => {
  it('offers releasing again after a failed, timed-out or unknown outcome that nothing has followed', () => {
    expect(canRetry({ kind: 'release', status: 'failed', force: false }, false)).toBe(true)
    expect(canRetry({ kind: 'release', status: 'needs_manual_confirmation', force: false }, false)).toBe(true)
    expect(canRetry({ kind: 'release', status: 'needs_manual_confirmation', force: false, next_release_id: 'j2' }, true)).toBe(false)
    expect(canRetry({ kind: 'release', status: 'succeeded', force: false }, true)).toBe(false)
    expect(canRetry({ kind: 'release', status: 'timeout', force: true }, false)).toBe(false)
    expect(canRetry({ kind: 'release', status: 'timeout', force: true }, true)).toBe(true)
    expect(canRetry({ kind: 'pre_release_query', status: 'failed', force: false }, true)).toBe(false)
  })

  it('asks the history for one page and leaves empty filters out', () => {
    expect(jobsPath({})).toBe('/api/releases/jobs')
    expect(jobsPath({ q: ' 10.0. ', kind: 'release', status: '', page: 1, pageSize: 8 })).toBe('/api/releases/jobs?q=10.0.&kind=release&page_size=8')
    expect(jobsPath({ status: 'needs_manual_confirmation', latest: true, page: 3 })).toBe('/api/releases/jobs?status=needs_manual_confirmation&latest=1&page=3')
  })

  it('lets operators and admins release, and nobody else', () => {
    const user = (roles: string[], superAdmin = false) => ({ superAdmin, roles: roles.map((name) => ({ name })) }) as unknown as User
    expect(canRelease(user(['Operator']))).toBe(true)
    expect(canRelease(user(['Admin']))).toBe(true)
    expect(canRelease(user([], true))).toBe(true)
    expect(canRelease(user(['Auditor']))).toBe(false)
  })
})
