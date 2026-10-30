import { describe, expect, it } from 'vitest'
import { entryLabel, phaseLabel, reasonLabel, releaseErrorText, statusLabel, targetIPProblem } from './release-console'

describe('release console messages', () => {
  it('checks the typed address before asking a switch', () => {
    expect(targetIPProblem('', true)).toBe('请输入要查询的 IPv4 地址。')
    expect(targetIPProblem('   ', false)).toBe('Enter the IPv4 address to query.')
    expect(targetIPProblem(' 10.20.30.41 ', true)).toBe('')
    expect(targetIPProblem('255.255.255.255', true)).toBe('')
    for (const value of ['10.20.30', '10.20.30.256', '10.20.30.041', '10.20.30.41/24', '2001:db8::1', 'core-sw']) {
      expect(targetIPProblem(value, true)).toBe('这不是有效的 IPv4 地址，请按 10.0.0.42 这样的格式输入。')
    }
  })

  it('explains refusals from the release API in the interface language', () => {
    expect(releaseErrorText(new Error('no enabled validated switch owns this IP'), true)).toBe('没有哪台已启用的交换机拥有包含该地址、且已验证的网段。管理员可以在“资产配置”中检查网段。')
    expect(releaseErrorText(new Error('switch configuration changed since the pre-query; query again'), true)).toBe('查询之后交换机配置已变更。请重新查询绑定状态后再释放。')
    expect(releaseErrorText(new Error('conflict'), false)).toMatch(/Query again\.$/)
    expect(releaseErrorText(new Error('operator role required'), true)).toBe('需要操作员或管理员角色。')
    expect(releaseErrorText(new Error('an unexpected detail'), true)).toBe('an unexpected detail')
    expect(releaseErrorText('not an error', true)).toBe('操作失败，请重试。')
  })

  it('names job states, phases, bindings and reasons instead of showing their codes', () => {
    expect(statusLabel('stopped_no_switch', true)).toBe('未匹配到交换机')
    expect(statusLabel('needs_manual_confirmation', false)).toBe('Needs manual confirmation')
    expect(statusLabel('some_new_state', true)).toBe('some new state')
    expect(phaseLabel('querying_after', true)).toBe('正在查询释放后状态')
    expect(entryLabel('dynamic', true)).toBe('动态记录')
    expect(entryLabel(undefined, true)).toBe('—')
    expect(reasonLabel('temporary_test', true)).toBe('临时测试')
    expect(reasonLabel('temporary_test', false)).toBe('Temporary test')
  })
})
