import { describe, expect, it } from 'vitest'
import { errorText, isConfigExcerpt, serverText } from './messages'

describe('server texts', () => {
  it('translates fixed API, import and worker texts', () => {
    expect(serverText('switch could not be saved', true)).toBe('交换机无法保存，名称或管理 IP 可能已被使用。')
    expect(serverText('switch could not be saved', false)).toBe('The switch could not be saved; its name or management IP may already be in use.')
    expect(serverText('a switch with this management IP already exists', true)).toBe('已存在相同管理 IP 的交换机。')
    expect(serverText('Timed out', true)).toBe('超时')
    expect(serverText('Static binding is not present after release', true)).toBe('解绑后静态绑定已不存在')
  })

  it('translates texts that carry values, including the server text inside them', () => {
    expect(serverText('Port 23 unreachable: Timed out', true)).toBe('端口 23 不可达：超时')
    expect(serverText('Rule "禁止开启 FTP 服务" failed: Pattern not found', true)).toBe('规则「禁止开启 FTP 服务」未通过：配置中未找到匹配内容')
    expect(serverText('Backup failed: Network error (OSError)', true)).toBe('配置备份失败：网络错误（OSError）')
    expect(serverText('cron hour field: "25" is outside 0-23', true)).toBe('cron 的「时」字段：「25」超出范围 0–23')
    expect(serverText('cron minute field: invalid step "*/0"', false)).toBe('Cron minute field: Invalid step "*/0"')
    expect(serverText('management_ip repeats line 3', true)).toBe('管理 IP 与第 3 行重复。')
    expect(serverText('Network read failed: No interface addresses in the configuration; networks kept', true)).toBe('读取网段失败：配置里没有接口地址，网段保持不变')
    expect(serverText('Networks changed: 2 added, 1 removed', true)).toBe('网段已更新：新增 2 个，移除 1 个')
    expect(serverText('Backup failed: Switch connection failed', true)).toBe('配置备份失败：连接设备失败')
    expect(serverText('Rule "禁止开启 FTP 服务" failed: Line 7: ftp server enable', true)).toBe('规则「禁止开启 FTP 服务」未通过：第 7 行：ftp server enable')
    expect(isConfigExcerpt('Line 7: ftp server enable')).toBe(true)
    expect(isConfigExcerpt('Pattern not found')).toBe(false)
  })

  it('shows technical detail it does not know as sent', () => {
    expect(serverText('invalid pattern: missing closing )', true)).toBe('正则表达式无效：missing closing )')
    expect(serverText('ftp server enable', true)).toBe('ftp server enable')
    expect(serverText(null, true)).toBe('')
    expect(errorText('not an error', true)).toBe('操作失败，请重试。')
    expect(errorText(new Error('login required'), false)).toBe('Your session has ended. Sign in again.')
  })

  it('words why the WeCom bot is not connected', () => {
    expect(serverText('WeCom refused the bot ID or secret (errcode 40001: invalid secret)', true)).toBe('企业微信拒绝了机器人 ID 或 Secret（错误码 40001：invalid secret），请核对后重新填写。')
    expect(serverText('cannot reach WeCom: dial tcp: i/o timeout', true)).toBe('连不上企业微信：dial tcp: i/o timeout。请确认服务器能访问长连接地址。')
    expect(serverText('another connection took over this bot; the sandbox and production each need their own bot', true)).toContain('沙箱和生产环境要各用一个机器人')
  })

  it('words why the Feishu bot is not connected', () => {
    expect(serverText('Feishu refused the App ID or secret (code 1000040346: )', true)).toBe('飞书拒绝了 App ID 或 App Secret（错误码 1000040346），请核对后重新填写。')
    expect(serverText('Feishu refused the App ID or secret (code 10014: app secret invalid)', false)).toBe('Feishu refused the App ID or secret (error 10014: app secret invalid). Check them and enter them again.')
    expect(serverText('cannot reach Feishu: HTTP 502', true)).toBe('连不上飞书：HTTP 502。请确认服务器能访问开放平台地址。')
    expect(serverText('Feishu stopped answering the heartbeat', true)).toBe('飞书不再回应心跳。')
  })
})
