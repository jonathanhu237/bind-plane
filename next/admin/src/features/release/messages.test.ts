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
    expect(serverText('cidr has host bits set; did you mean 10.0.0.0/24?', true)).toBe('网段的主机位不为 0，是否应为 10.0.0.0/24？')
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
})
