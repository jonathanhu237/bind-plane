/**
 * Texts from the Bind Plane API and worker, which answer in English, in the interface
 * language. Unknown text is shown as sent: it is technical detail such as an RE2 error.
 */

type Words = [english: string, chinese: string]

const unavailable: Words = ['The service is unavailable right now. Try again shortly.', '服务暂时不可用，请稍后重试。']
const unreadable: Words = ['The request could not be read. Refresh the page and try again.', '请求无法识别，请刷新页面后重试。']
const rejectedRow: Words = ['The database rejected this row.', '数据库拒绝了这一行。']
const tooShort: Words = ['The backup output is too short to be a configuration.', '备份输出过短，不像完整配置。']

const fixed: Record<string, Words> = {
  // API refusals.
  'invalid request': unreadable, 'JSON required': unreadable, 'exactly one JSON object required': unreadable,
  'invalid filter': ['These filters cannot be used. Clear them and try again.', '筛选条件无效，请清除后重试。'],
  'login required': ['Your session has ended. Sign in again.', '登录已失效，请重新登录。'],
  'invalid origin': ['The request was refused because it came from another site.', '请求来源不受信任，已被拒绝。'],
  'admin role required': ['This needs the Admin role.', '需要管理员角色。'],
  'operator role required': ['This needs the Operator or Admin role.', '需要操作员或管理员角色。'],
  'release reason required': ['Choose a release reason.', '请选择解绑原因。'],
  'not found': ['It no longer exists, or you cannot see it.', '对象不存在，或你无权查看。'],
  conflict: ['It changed in the meantime. Reload and try again.', '内容已被修改，请刷新后重试。'],
  'service unavailable': unavailable, 'asset store unavailable': unavailable, 'asset audit unavailable': unavailable,
  'device operations store unavailable': unavailable,
  'encryption unavailable': ['Credential encryption is not configured on the server.', '服务器未配置凭据加密密钥。'],
  'asset changed since it was loaded; reload and try again': [
    'Someone changed this asset after you opened it. Close the editor and open it again to edit the latest version.',
    '打开后该资产已被其他人修改。请关闭编辑框后重新打开，在最新内容上修改。',
  ],
  'change conflicts with existing assets': ['Another asset already uses this name or address, or a selected item no longer exists.', '名称或地址已被其他资产使用，或所选对象已不存在。'],
  'credential could not be saved': ['The credential could not be saved; its name may already be in use.', '凭据无法保存，名称可能已被使用。'],
  'profile could not be saved': ['The command profile could not be saved; its name may already be in use.', '命令模板无法保存，名称可能已被使用。'],
  'group could not be saved': ['The group could not be saved; its name may already be in use.', '分组无法保存，名称可能已被使用。'],
  'switch could not be saved': ['The switch could not be saved; its name or management IP may already be in use.', '交换机无法保存，名称或管理 IP 可能已被使用。'],
  'rule could not be saved (duplicate name or missing group)': ['The rule could not be saved: its name is taken, or its group no longer exists.', '规则无法保存：名称已被使用，或所选分组已不存在。'],
  'schedule could not be saved (duplicate name or missing group)': ['The schedule could not be saved: its name is taken, or its group no longer exists.', '计划任务无法保存：名称已被使用，或所选分组已不存在。'],
  'group not found': ['That group no longer exists.', '该分组已不存在。'],
  'alert is not open': ['This alert has already been handled.', '该告警已被处理。'],
  'both versions must exist and belong to one switch': ['Both configuration versions must exist and belong to the same switch.', '两个配置版本都必须存在，并且属于同一台交换机。'],
  'credential name, username and password required': ['Name, device username and device password are required.', '名称、设备用户名和设备密码都必须填写。'],
  'current version required': ['The version being edited is unknown. Reload and try again.', '无法确定正在编辑的版本，请刷新后重试。'],
  'group name required': ['Enter a group name.', '请输入分组名称。'],
  'invalid credential': ['The credential is not valid.', '凭据内容无效。'],
  'invalid group': ['The group is not valid.', '分组内容无效。'],
  'invalid profile': ['The command profile is not valid.', '命令模板内容无效。'],
  'invalid rule': ['The rule is not valid.', '规则内容无效。'],
  'invalid schedule': ['The schedule is not valid.', '计划任务内容无效。'],
  'invalid switch asset': ['The switch is not valid. Check the management IP, port and selections.', '交换机信息无效，请检查管理 IP、端口和各项选择。'],
  'kind switches and CSV text required': ['Paste the CSV text of the switches to import.', '请粘贴要导入的交换机 CSV 内容。'],
  'kind and a scope (all, group or switches) required': ['Choose a task type and the switches to run it on.', '请选择任务类型和执行范围。'],
  'name, kind and scope (all, or group with a group) required': ['Name, task type and scope are required; a group scope needs a group.', '名称、任务类型和范围都必须填写；按分组执行时需要选择分组。'],
  'name, mode, severity and optional vendor or group required': ['Name, check mode and severity are required.', '名称、判定方式和严重级别都必须填写。'],
  'patterns must be arrays': ['Pattern lists must be JSON arrays, such as ["Error:"].', '规则列表必须是 JSON 数组，例如 ["Error:"]。'],
  'profile rules must be objects': ['Commands and parsing rules must be JSON objects.', '命令和解析规则必须是 JSON 对象。'],
  'profile rules must be objects and patterns must be arrays': ['Rules must be JSON objects, and pattern lists JSON arrays.', '规则必须是 JSON 对象，规则列表必须是 JSON 数组。'],
  'cron expression needs five fields: minute hour day month weekday': ['A cron expression has five fields: minute hour day month weekday.', 'cron 表达式需要 5 个字段：分 时 日 月 周。'],
  'cron expression never matches': ['This cron expression never runs.', '这个 cron 表达式永远不会触发。'],
  'pattern must be 1-2000 characters': ['The pattern must be 1 to 2000 characters long.', '匹配规则长度必须在 1 到 2000 个字符之间。'],
  'CSV header row required': ['The CSV needs a header row.', 'CSV 需要表头行。'],
  'CSV has no data rows': ['The CSV has no data rows.', 'CSV 没有数据行。'],
  // Import rows.
  'name is required': ['Name is missing.', '缺少名称。'],
  'port must be 1-65535': ['Port must be between 1 and 65535.', '端口必须在 1–65535 之间。'],
  'management_ip must be one canonical IPv4 address': ['Management IP must be one IPv4 address, such as 10.0.0.1.', '管理 IP 必须是一个 IPv4 地址，例如 10.0.0.1。'],
  'vendor must be huawei, h3c, ruijie, cisco or other': ['Vendor must be huawei, h3c, ruijie, cisco or other.', '厂商必须是 huawei、h3c、ruijie、cisco 或 other。'],
  'protocol must be telnet or ssh': ['Protocol must be telnet or ssh.', '登录协议必须是 telnet 或 ssh。'],
  'credential not found (give its exact name, or choose a default)': ['Credential not found. Give its exact name, or choose a default.', '找不到凭据。请填写准确名称，或选择默认凭据。'],
  'command profile not found (give its exact name, or choose a default)': ['Command profile not found. Give its exact name, or choose a default.', '找不到命令模板。请填写准确名称，或选择默认模板。'],
  'command profile is for another vendor': ['The command profile is for another vendor. Choose one of this switch’s vendor.', '命令模板属于其他厂商，请选择这台交换机所属厂商的模板。'],
  'switches of another vendor use this command profile': ['Switches of another vendor use this command profile. Move them to another profile before changing its vendor.', '有其他厂商的交换机在用这个命令模板。先把它们换到别的模板，再修改厂商。'],
  'no single command profile for this vendor (name one, or choose a default of this vendor)': ['This vendor has no single command profile. Name one in the profile column, or choose a default of this vendor.', '这个厂商没有唯一的命令模板。请在“命令模板”列填写，或选择一个该厂商的默认模板。'],
  'a switch with this management IP already exists': ['A switch with this management IP already exists.', '已存在相同管理 IP 的交换机。'],
  'already up to date': ['Already matches the CSV.', '与 CSV 一致，无需修改。'],
  'switches and exactly one change (group, site, credential, enabled or release role) required': ['Choose switches and one change to make.', '请选择设备和一项要做的修改。'],
  'credential is disabled or gone': ['That credential is disabled or no longer exists. Choose an enabled one.', '这条凭据已停用或已不存在，请选择一条启用的凭据。'],
  'group is used by schedules or compliance rules': [
    'A schedule or compliance rule targets this group. Change them first; otherwise they would run on every switch.',
    '有计划任务或合规规则限定在这个分组。请先修改它们，否则它们会变成对所有交换机生效。',
  ],
  'the database rejected this row': rejectedRow, 'row rejected by the database': rejectedRow,
  'handles_release must be yes or no': ['"handles_release" must be yes or no.', '“负责解绑”必须填“是”或“否”。'],
  // Device operations in the worker.
  'New configuration version': ['New configuration version stored', '已保存新的配置版本'],
  'Configuration unchanged': ['Configuration unchanged', '配置无变化'],
  'Already queued or running': ['Already queued or running; skipped', '已在队列中，本次跳过'],
  'Switch is disabled': ['Switch is disabled', '设备已停用'],
  'Worker stopped before finishing': ['The worker stopped before finishing', '工作进程中断，未完成'],
  'Unexpected worker error': ['Unexpected worker error', '工作进程异常'],
  'Signed in': ['Signed in and out; nothing was sent', '登录成功，未执行任何命令'],
  'Credential or command profile is disabled': ['Credential or command profile is disabled', '凭据或命令模板已停用'],
  'Command profile has no config_backup command': ['The command profile has no config_backup command', '命令模板未配置 config_backup 命令'],
  'Backup output is too short to be a configuration': tooShort, 'Backup output is too short': tooShort,
  'Backup output matches an error pattern of the command profile': ['The backup output matches an error pattern of the command profile', '备份输出匹配到命令模板的错误规则'],
  'Configuration is larger than 20 MB': ['The configuration is larger than 20 MB', '配置超过 20 MB'],
  'Configuration read failed': ['Reading the configuration failed', '读取配置失败'],
  'Configuration could not be decrypted': ['The configuration could not be decrypted', '配置无法解密'],
  'Connection refused': ['Connection refused', '连接被拒绝'],
  'Networks unchanged': ['Networks unchanged', '网段没有变化'],
  'Switch does not handle releases': ['The switch does not handle releases', '这台交换机不负责解绑'],
  'Configuration does not end with return or end': ['The configuration read was cut short; networks kept', '读到的配置不完整，网段保持不变'],
  'No interface addresses in the configuration; networks kept': ['The configuration has no interface addresses; networks kept', '配置里没有接口地址，网段保持不变'],
  'Timed out': ['Timed out', '超时'],
  'Switch authentication failed': ['Switch authentication failed', '设备认证失败'],
  'Switch connection timed out': ['Switch connection timed out', '连接设备超时'],
  'Switch connection failed': ['Switch connection failed', '连接设备失败'],
  'Switch command failed': ['A switch command failed', '设备命令执行失败'],
  'Switch command timed out': ['A switch command timed out', '设备命令执行超时'],
  'Switch paging command failed': ['The paging command failed', '关闭分页的命令执行失败'],
  'Switch paging command timed out': ['The paging command timed out', '关闭分页的命令执行超时'],
  'Configuration read timed out': ['Reading the configuration timed out', '读取配置超时'],
  'Release job timed out': ['The release job timed out', '解绑任务超时'],
  'Pattern not found': ['Pattern not found in the configuration', '配置中未找到匹配内容'],
  'no configuration backups to export': ['None of these switches has a configuration backup yet.', '这些设备还没有配置备份。'],
  'ids must be at most 1000 switch IDs': ['Choose at most 1000 switches, or export by filter instead.', '一次最多选 1000 台设备，更多的请按筛选条件导出。'],
  // Release jobs in the API, the worker and its result classifier.
  'Release queued': ['Release queued', '已提交解绑，等待执行'],
  'Retry queued as a new release': ['Released again; queued as a new release', '重新解绑：已作为新的解绑排队'],
  'Dynamic entry is not a static binding': ['Only a dynamic entry, so the address is not bound; nothing was sent', '只有动态记录，没有绑定，未发送任何命令'],
  'No record; release stopped': ['The switch has no record of this address, so nothing was sent. Check the address, and that its network belongs to the right switch and command profile.', '交换机上查不到这个 IP 的记录，未发送任何命令。请检查 IP 是否输错，以及它所在网段登记的交换机和命令模板是否正确。'],
  'Pre-query could not be parsed; nothing was sent': ['The output before release could not be parsed, so nothing was sent', '无法解析解绑前的查询输出，没有执行解绑'],
  'Switch configuration changed after the release was queued; release again': ['The switch configuration changed after the release was queued. Release again.', '提交解绑后交换机配置有变更，请重新解绑。'],
  // Messages kept on jobs from the old two-step flow, which queried first and released after a confirmation.
  'Pre-release query queued': ['Pre-release query queued', '已提交解绑前查询'],
  'Release confirmed and queued': ['Release confirmed and queued', '已确认解绑，等待执行'],
  'Retry begins with a fresh pre-query': ['Retry begins with a fresh pre-query', '重试：重新查询解绑前状态'],
  'Pre-query ready for confirmation': ['Pre-query ready for confirmation', '查询完成'],
  'No record; operator must stop': ['The switch has no record of this address', '交换机上没有这个 IP 的记录'],
  'Pre-query could not be parsed': ['The pre-query output could not be parsed', '无法解析查询输出'],
  'Switch configuration could not be used': ['The switch configuration could not be used', '无法使用该交换机的配置'],
  'Worker lease expired': ['Worker lease expired', '执行器租约已过期'],
  'Worker lease expired; switch state is uncertain': ['Worker lease expired; the switch state is uncertain', '执行器租约已过期，交换机状态不确定'],
  'Release command returned an error pattern': ['The release command returned an error pattern', '解绑命令的输出匹配到错误规则'],
  'Static binding is not present after release': ['The static binding is gone after release', '解绑后静态绑定已不存在'],
  'Static binding is still present after release': ['The static binding is still present after release', '解绑后静态绑定仍然存在'],
  'Post-release state could not be parsed confidently': ['The state after release could not be parsed confidently, so the outcome is unknown', '无法可靠解析解绑后的状态，结果未知'],
}

const cronFields: Record<string, Words> = { minute: ['minute', '分'], hour: ['hour', '时'], day: ['day', '日'], month: ['month', '月'], weekday: ['weekday', '周'] }

// Texts carrying values; nested server text inside them is translated too.
const shaped: [RegExp, (zh: boolean, ...parts: string[]) => string][] = [
  [/^cron (minute|hour|day|month|weekday) field: (.+)$/, (zh, field, detail) =>
    zh ? `cron 的「${cronFields[field][1]}」字段：${serverText(detail, zh)}` : `Cron ${field} field: ${serverText(detail, zh)}`],
  [/^"(.*)" is outside (\d+)-(\d+)$/, (zh, value, low, high) => (zh ? `「${value}」超出范围 ${low}–${high}` : `"${value}" is outside ${low}–${high}`)],
  [/^invalid (range|step|value) "(.*)"$/, (zh, part, value) =>
    zh ? `${{ range: '范围', step: '步长', value: '取值' }[part]}「${value}」无效` : `Invalid ${part} "${value}"`],
  [/^invalid pattern: (.+)$/s, (zh, detail) => (zh ? `正则表达式无效：${detail}` : `Invalid pattern: ${detail}`)],
  [/^CSV is malformed: (.+)$/s, (zh, detail) => (zh ? `CSV 格式有误：${detail}` : `The CSV is malformed: ${detail}`)],
  [/^at most (\d+) rows per import$/, (zh, rows) => (zh ? `每次最多导入 ${rows} 行。` : `At most ${rows} rows per import.`)],
  [/^management_ip repeats line (\d+)$/, (zh, line) => (zh ? `管理 IP 与第 ${line} 行重复。` : `Management IP repeats line ${line}.`)],
  [/^Networks changed: (\d+) added, (\d+) removed$/, (zh, added, removed) => (zh ? `网段已更新：新增 ${added} 个，移除 ${removed} 个` : `Networks updated: ${added} added, ${removed} removed`)],
  [/^The configuration would remove (\d+) of (\d+) networks; networks kept$/, (zh, lost, total) => (zh
    ? `配置里少了 ${lost} 个网段（原有 ${total} 个），为防误删保持不变。确认交换机配置无误的话，在设备详情里点“刷新网段”。`
    : `The configuration would remove ${lost} of ${total} networks, so they were kept. If the switch is right, use Refresh networks in the device details.`)],
  [/^Network read failed: (.+)$/s, (zh, why) => (zh ? `读取网段失败：${serverText(why, zh)}` : `Network read failed: ${serverText(why, zh)}`)],
  [/^Port (\d+) unreachable: (.+)$/s, (zh, port, why) => (zh ? `端口 ${port} 不可达：${serverText(why, zh)}` : `Port ${port} unreachable: ${serverText(why, zh)}`)],
  [/^changes: (.+)$/, (zh, fields) => {
    const names: Record<string, Words> = {
      name: ['name', '名称'], vendor: ['vendor', '厂商'], model: ['model', '型号'], group: ['group', '分组'], site: ['site', '校区'],
      protocol: ['protocol', '协议'], port: ['port', '端口'], credential: ['credential', '凭据'], profile: ['command profile', '命令模板'], description: ['description', '描述'],
      handles_release: ['release role', '负责解绑'],
    }
    const list = fields.split(', ').map((field) => names[field]?.[zh ? 1 : 0] ?? field)
    return zh ? `将修改：${list.join('、')}` : `Changes: ${list.join(', ')}`
  }],
  [/^Backup failed: (.+)$/s, (zh, why) => (zh ? `配置备份失败：${serverText(why, zh)}` : `Backup failed: ${serverText(why, zh)}`)],
  [/^Rule "(.*)" failed: (.+)$/s, (zh, rule, detail) => (zh ? `规则「${rule}」未通过：${serverText(detail, zh)}` : `Rule "${rule}" failed: ${serverText(detail, zh)}`)],
  [/^Line (\d+): (.*)$/s, (zh, line, excerpt) => (zh ? `第 ${line} 行：${excerpt}` : `Line ${line}: ${excerpt}`)],
  [/^Switch session close failed: (\w+)$/, (zh, name) => (zh ? `关闭设备会话失败（${name}）` : `Closing the switch session failed (${name})`)],
  [/^Network error \((\w+)\)$/, (zh, name) => (zh ? `网络错误（${name}）` : `Network error (${name})`)],
  [/^Unexpected error \((\w+)\)$/, (zh, name) => (zh ? `意外错误（${name}）` : `Unexpected error (${name})`)],
]

export function serverText(message: string | null | undefined, zh: boolean): string {
  if (!message) return ''
  const words = fixed[message]
  if (words) return words[zh ? 1 : 0]
  for (const [pattern, render] of shaped) {
    const match = pattern.exec(message)
    if (match) return render(zh, ...match.slice(1))
  }
  return message
}

/** Compliance evidence that quotes the configuration ("Line 7: …"), shown as code. */
export const isConfigExcerpt = (detail: string | null | undefined) => /^Line \d+: /.test(detail ?? '')

/** The scope a device task ran on, as the API labels it, in the interface language. */
export function taskScopeText(label: string, zh: boolean): string {
  if (!zh) return label
  if (label === 'All enabled switches') return '所有启用的交换机'
  const match = /^(?:Group (.+)|(\d+) selected switches|Switch (.+))$/s.exec(label)
  if (!match) return label
  return match[1] !== undefined ? `分组「${match[1]}」` : match[2] !== undefined ? `选中的 ${match[2]} 台交换机` : `交换机 ${match[3]}`
}

/** An error from a Bind Plane request, in the interface language. */
export function errorText(error: unknown, zh: boolean): string {
  return error instanceof Error && error.message ? serverText(error.message, zh) : zh ? '操作失败，请重试。' : 'Something went wrong. Try again.'
}
