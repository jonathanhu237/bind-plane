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
  'admin force required': ['Only an admin can force a release.', '只有管理员可以强制解绑。'],
  'preparation job required': ['Query the binding first.', '请先查询绑定状态。'],
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
  'network could not be saved': ['The network could not be saved: it may already exist, or its switch is gone.', '网段无法保存：它可能已经存在，或所属交换机已不存在。'],
  'switch could not be saved': ['The switch could not be saved; its name or management IP may already be in use.', '交换机无法保存，名称或管理 IP 可能已被使用。'],
  'rule could not be saved (duplicate name or missing group)': ['The rule could not be saved: its name is taken, or its group no longer exists.', '规则无法保存：名称已被使用，或所选分组已不存在。'],
  'schedule could not be saved (duplicate name or missing group)': ['The schedule could not be saved: its name is taken, or its group no longer exists.', '计划任务无法保存：名称已被使用，或所选分组已不存在。'],
  'group not found': ['That group no longer exists.', '该分组已不存在。'],
  'alert is not open': ['This alert has already been handled.', '该告警已被处理。'],
  'both versions must exist and belong to one switch': ['Both configuration versions must exist and belong to the same switch.', '两个配置版本都必须存在，并且属于同一台交换机。'],
  'canonical IPv4 CIDR required': ['Enter an IPv4 network such as 10.0.0.0/24, with the host bits set to zero.', '请输入 IPv4 网段，例如 10.0.0.0/24（主机位必须为 0）。'],
  'credential name, username and password required': ['Name, device username and device password are required.', '名称、设备用户名和设备密码都必须填写。'],
  'current version required': ['The version being edited is unknown. Reload and try again.', '无法确定正在编辑的版本，请刷新后重试。'],
  'group name required': ['Enter a group name.', '请输入分组名称。'],
  'invalid credential': ['The credential is not valid.', '凭据内容无效。'],
  'invalid group': ['The group is not valid.', '分组内容无效。'],
  'invalid network': ['The network is not valid.', '网段内容无效。'],
  'invalid profile': ['The command profile is not valid.', '命令模板内容无效。'],
  'invalid rule': ['The rule is not valid.', '规则内容无效。'],
  'invalid schedule': ['The schedule is not valid.', '计划任务内容无效。'],
  'invalid switch asset': ['The switch is not valid. Check the management IP, port and selections.', '交换机信息无效，请检查管理 IP、端口和各项选择。'],
  'kind (switches or networks) and CSV text required': ['Choose what to import and paste the CSV text.', '请选择导入类型，并粘贴 CSV 内容。'],
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
  'a switch with this management IP already exists': ['A switch with this management IP already exists.', '已存在相同管理 IP 的交换机。'],
  'the database rejected this row': rejectedRow, 'row rejected by the database': rejectedRow,
  'validated must be true or false': ['"validated" must be true or false.', '“已验证”必须是 true 或 false。'],
  'cidr must be an IPv4 network such as 10.0.0.0/24': ['The network must be an IPv4 network, such as 10.0.0.0/24.', '网段必须是 IPv4 网段，例如 10.0.0.0/24。'],
  'several switches share this name; use the management IP': ['Several switches share this name; use the management IP instead.', '有多台交换机同名，请改用管理 IP。'],
  'switch not found (give its management IP or exact name)': ['Switch not found. Give its management IP or exact name.', '找不到交换机。请填写管理 IP 或准确名称。'],
  'this switch already owns this network': ['This switch already owns this network.', '该交换机已拥有这个网段。'],
  'another switch also owns this network; resolution stays ambiguous until corrected': [
    'Another switch also owns this network; releases in it stop until that is corrected.',
    '另一台交换机也拥有这个网段；修正之前，该网段内的解绑会因无法确定交换机而停止。',
  ],
  // Device operations in the worker.
  'New configuration version': ['New configuration version stored', '已保存新的配置版本'],
  'Configuration unchanged': ['Configuration unchanged', '配置无变化'],
  'Already queued or running': ['Already queued or running; skipped', '已在队列中，本次跳过'],
  'Switch is disabled': ['Switch is disabled', '设备已停用'],
  'Worker stopped before finishing': ['The worker stopped before finishing', '工作进程中断，未完成'],
  'Unexpected worker error': ['Unexpected worker error', '工作进程异常'],
  'Credential or command profile is disabled': ['Credential or command profile is disabled', '凭据或命令模板已停用'],
  'Command profile has no config_backup command': ['The command profile has no config_backup command', '命令模板未配置 config_backup 命令'],
  'Backup output is too short to be a configuration': tooShort, 'Backup output is too short': tooShort,
  'Backup output matches an error pattern of the command profile': ['The backup output matches an error pattern of the command profile', '备份输出匹配到命令模板的错误规则'],
  'Configuration is larger than 20 MB': ['The configuration is larger than 20 MB', '配置超过 20 MB'],
  'Configuration read failed': ['Reading the configuration failed', '读取配置失败'],
  'Configuration could not be decrypted': ['The configuration could not be decrypted', '配置无法解密'],
  'Connection refused': ['Connection refused', '连接被拒绝'],
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
  // Release jobs in the API, the worker and its result classifier.
  'Pre-release query queued': ['Pre-release query queued', '已提交解绑前查询'],
  'Release confirmed and queued': ['Release confirmed and queued', '已确认解绑，等待执行'],
  'Retry begins with a fresh pre-query': ['Retry begins with a fresh pre-query', '重试：重新查询解绑前状态'],
  'Pre-query ready for confirmation': ['Pre-query ready for confirmation', '查询完成，等待确认'],
  'Dynamic entry is not a static binding': ['Dynamic entry, not a static binding', '这是动态记录，不是静态绑定，无需解绑'],
  'No record; operator must stop': ['No record; an operator stops here, an admin may force the release', '交换机上没有该地址的记录；操作员到此为止，管理员可以强制解绑'],
  'No record; release stopped': ['No record; release stopped', '交换机上没有该地址的记录，解绑已停止'],
  'Pre-query could not be parsed': ['The pre-query output could not be parsed', '无法解析查询输出，需要人工确认'],
  'Switch configuration could not be used': ['The switch configuration could not be used', '无法使用该交换机的配置'],
  'Worker lease expired': ['Worker lease expired', '执行器租约已过期'],
  'Worker lease expired; switch state is uncertain': ['Worker lease expired; the switch state is uncertain', '执行器租约已过期，交换机状态不确定'],
  'Release command returned an error pattern': ['The release command returned an error pattern', '解绑命令的输出匹配到错误规则'],
  'Static binding is not present after release': ['The static binding is gone after release', '解绑后静态绑定已不存在'],
  'Static binding is still present after release': ['The static binding is still present after release', '解绑后静态绑定仍然存在'],
  'Post-release state could not be parsed confidently': ['The post-release state could not be parsed confidently', '无法可靠解析解绑后的状态，需要人工确认'],
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
  [/^(management_ip|network) repeats line (\d+)$/, (zh, what, line) =>
    zh ? `${what === 'network' ? '网段' : '管理 IP '}与第 ${line} 行重复。` : `${what === 'network' ? 'Network' : 'Management IP'} repeats line ${line}.`],
  [/^cidr has host bits set; did you mean (.+)\?$/, (zh, network) => (zh ? `网段的主机位不为 0，是否应为 ${network}？` : `The network has host bits set; did you mean ${network}?`)],
  [/^Port (\d+) unreachable: (.+)$/s, (zh, port, why) => (zh ? `端口 ${port} 不可达：${serverText(why, zh)}` : `Port ${port} unreachable: ${serverText(why, zh)}`)],
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
