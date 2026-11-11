import type { NetworkAsset, Vendor } from '@/features/release/asset-editors'
import { saveBlob } from './csv'

export class OpsRequestError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

/** Calls the admin-only device operations API. */
export async function opsRequest<T>(path: string, body?: unknown, method = body === undefined ? 'GET' : 'POST'): Promise<T> {
  const response = await fetch(`/api/ops/${path}`, {
    method, credentials: 'same-origin', cache: 'no-store',
    headers: { Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data: unknown = await response.json().catch(() => ({}))
  if (!response.ok) throw new OpsRequestError((data as { detail?: string }).detail || `HTTP ${response.status}`, response.status)
  return data as T
}

/** The file name a Content-Disposition header gives, preferring its UTF-8 form. */
export function attachmentName(header: string | null): string | null {
  if (!header) return null
  const encoded = /filename\*=utf-8''([^;]+)/i.exec(header)
  if (encoded) {
    try { return decodeURIComponent(encoded[1].trim()) } catch { /* fall back to the plain name */ }
  }
  const plain = /filename="((?:[^"\\]|\\.)*)"/i.exec(header) ?? /filename=([^;]+)/i.exec(header)
  return plain ? plain[1].replace(/\\(.)/g, '$1').trim() : null
}

/** Saves a file the device operations API sends as an attachment; a refusal throws like opsRequest. */
export async function opsDownload(path: string): Promise<void> {
  const response = await fetch(`/api/ops/${path}`, { credentials: 'same-origin', cache: 'no-store' })
  if (!response.ok) {
    const data: unknown = await response.json().catch(() => ({}))
    throw new OpsRequestError((data as { detail?: string }).detail || `HTTP ${response.status}`, response.status)
  }
  saveBlob(attachmentName(response.headers.get('Content-Disposition')) ?? 'download', await response.blob())
}

export type TaskKind = 'config_backup' | 'login_check' | 'network_read'

export type OpsSwitch = {
  id: string; name: string; management_ip: string; vendor: Vendor; model: string | null; site: string | null; description: string | null
  group_id: string | null; group_name: string | null; protocol: 'telnet' | 'ssh'; port: number | null; is_enabled: boolean; version: number
  credential_id: string; credential_name: string; command_profile_id: string; profile_name: string
  login_ok: boolean | null; login_checked_at: string | null; login_error: string | null
  config_checked_at: string | null; config_succeeded_at: string | null; config_error: string | null; compliance_failed: number
  /** Enabled, validated networks: the ones release resolution can use. */
  networks: number
  /** A release switch's networks are read from its configuration. */
  handles_release: boolean; networks_checked_at: string | null; networks_succeeded_at: string | null; networks_error: string | null
  /** The software version the newest backup or network read named; null until one has. */
  os_version: string | null
}
export type Page<T> = { items: T[]; total: number; page_size: number }
export type SwitchPage = Page<OpsSwitch> & { sites: string[]; versions: { version: string; count: number }[] }

export type TaskSummary = {
  id: string; kind: TaskKind; scope_label: string; actor_name: string | null; actor_email: string | null; schedule_name: string | null; created_at: string
  total: number; queued: number; running: number; succeeded: number; failed: number; skipped: number; changed: number; finished_at: string | null
}
export type TaskItem = {
  id: number; switch_id: string; switch_name: string; management_ip: string; status: 'queued' | 'running' | 'succeeded' | 'failed' | 'skipped'
  message: string | null; changed: boolean | null; backup_id: number | null; started_at: string | null; finished_at: string | null
}
export type TaskDetail = TaskSummary & { items: TaskItem[] }

export type Schedule = {
  id: string; name: string; kind: TaskKind; cron: string; scope: 'all' | 'group'; group_id: string | null; group_name: string | null
  is_enabled: boolean; next_run_at: string | null; last_run_at: string | null; version: number
}
export type Severity = 'info' | 'warning' | 'critical'
export type Rule = {
  id: string; name: string; description: string | null; pattern: string; mode: 'must_contain' | 'must_not_contain'; severity: Severity
  vendor: Vendor | null; group_id: string | null; group_name: string | null; is_enabled: boolean; version: number; passed: number; failed: number
}
export type ComplianceResult = {
  switch_id: string; switch_name: string; management_ip: string; rule_id: string; rule_name: string; severity: Severity
  passed: boolean; detail: string | null; evaluated_at: string; backup_id: number
}
export type Alert = {
  id: number; kind: 'backup_failed' | 'compliance_violation' | 'network_read_failed'; severity: Severity; switch_id: string; switch_name: string
  management_ip: string; rule_id: string | null; rule_name: string | null; message: string; status: 'open' | 'acknowledged' | 'resolved'
  opened_at: string; last_seen_at: string; acknowledged_at: string | null; acknowledged_by: string | null; resolved_at: string | null
}
export type AuditEvent = { id: number; actor_name: string | null; actor_email: string; action: string; object_id: string; object_name: string; changes: Record<string, unknown>; created_at: string }

export type Overview = {
  switches: { total: number; enabled: number; login_ok: number; login_failed: number; untested: number }
  backups: { backed_up: number; failing: number; never: number; versions_24h: number }
  compliance: { passed: number; failed: number; failing_switches: number }
  alerts: { severity: Severity; count: number }[]
  recent_tasks: TaskSummary[]
  recent_changes: { id: number; switch_id: string; switch_name: string; management_ip: string; captured_at: string; line_count: number }[]
  upcoming: { id: string; name: string; kind: TaskKind; next_run_at: string }[]
  /** The last 30 days, oldest first: switches whose configuration changed, and whose backup failed. */
  trend: TrendDay[]
}

/** The chat platforms members release from. */
export type ChatPlatform = 'wecom' | 'feishu'
export type ChatBotState = 'disabled' | 'connecting' | 'connected' | 'failed'
/** A chat bot's page: app_id is WeCom's bot ID or Feishu's App ID, address its long-connection address or open platform. */
export type ChatBotPage = {
  settings: {
    id: string; is_enabled: boolean; app_id: string; has_secret: boolean; address: string; version: number
    state: ChatBotState; state_at: string | null; connected_at: string | null; last_error: string | null
  }
  accounts: { user_id: string; name: string; email: string; disabled: boolean; external_id: string; created_at: string }[]
  users: { id: string; name: string; email: string }[]
}

export type ConfigMatch = { line: number; text: string; section: string }
export type ConfigSearch = {
  searched: number; without_backup: number; matched: number; lines: number; truncated: boolean
  results: { backup_id: number; switch_id: string; switch_name: string; management_ip: string; group_name: string | null; captured_at: string; count: number; matches: ConfigMatch[] }[]
}

export type TrendDay = { date: string; changed: number; backup_failed: number }
export type DayChanges = {
  date: string
  changed: { backup_id: number; previous_id: number; switch_id: string; switch_name: string; management_ip: string; captured_at: string; line_count: number }[]
  backup_failed: { switch_id: string; switch_name: string; management_ip: string; finished_at: string; message: string | null }[]
}

export type BackupMeta = { id: number; captured_at: string; size_bytes: number; line_count: number; sha256: string }
export type HistoryEvent = { id: number; action: string; actor_name: string | null; actor_email: string; object_name: string; changes: Record<string, unknown>; created_at: string }
export type SwitchDetail = Omit<OpsSwitch, 'group_id' | 'credential_id' | 'command_profile_id' | 'version' | 'compliance_failed' | 'networks'> & {
  backups: BackupMeta[]
  compliance: { rule_id: string; rule_name: string; severity: Severity; mode: string; passed: boolean; detail: string | null; evaluated_at: string; backup_id: number }[]
  recent_items: { id: number; kind: TaskKind; status: TaskItem['status']; message: string | null; changed: boolean | null; backup_id: number | null; created_at: string; finished_at: string | null }[]
  alerts: { id: number; kind: Alert['kind']; severity: Severity; message: string; status: Alert['status']; opened_at: string }[]
  networks: NetworkAsset[]
  /** Changes to the switch and to the networks it owns, newest first. */
  history: HistoryEvent[]
}
export type DiffLine = { kind: 'context' | 'add' | 'remove'; text: string }
export type DiffResponse = {
  switch_name: string
  base: { id: number; captured_at: string }
  target: { id: number; captured_at: string }
  diff: { added: number; removed: number; replaced: boolean; hunks: { old_start: number; old_lines: number; new_start: number; new_lines: number; lines: DiffLine[] }[] }
}

export type TaskState = 'running' | 'succeeded' | 'partial' | 'failed' | 'empty'

export function taskState(task: Pick<TaskSummary, 'total' | 'queued' | 'running' | 'succeeded' | 'failed' | 'skipped'>): TaskState {
  if (task.queued + task.running > 0) return 'running'
  if (task.total === 0 || task.total === task.skipped) return 'empty'
  if (task.failed > 0) return task.succeeded > 0 ? 'partial' : 'failed'
  return 'succeeded'
}

/** A plain-language reading of common cron shapes; anything else is shown as written. */
export function describeCron(expression: string, zh: boolean): string {
  const [minute, hour, day, month, weekday] = expression.trim().split(/\s+/)
  const pad = (value: string) => value.padStart(2, '0')
  if (expression.trim().split(/\s+/).length !== 5 || day !== '*' || month !== '*') return expression
  const every = /^\*\/(\d+)$/
  if (minute === '*' && hour === '*' && weekday === '*') return zh ? '每分钟' : 'Every minute'
  if (hour === '*' && weekday === '*' && every.test(minute)) return zh ? `每 ${every.exec(minute)![1]} 分钟` : `Every ${every.exec(minute)![1]} minutes`
  if (/^\d+$/.test(minute) && every.test(hour) && weekday === '*') return zh ? `每 ${every.exec(hour)![1]} 小时（第 ${minute} 分）` : `Every ${every.exec(hour)![1]} hours at :${pad(minute)}`
  if (/^\d+$/.test(minute) && hour === '*' && weekday === '*') return zh ? `每小时第 ${minute} 分` : `Hourly at :${pad(minute)}`
  if (/^\d+$/.test(minute) && /^\d+$/.test(hour)) {
    const time = `${pad(hour)}:${pad(minute)}`
    if (weekday === '*') return zh ? `每天 ${time}` : `Daily at ${time}`
    if (/^[0-7]$/.test(weekday)) {
      const names = zh ? ['周日', '周一', '周二', '周三', '周四', '周五', '周六', '周日'] : ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']
      return zh ? `每${names[Number(weekday)]} ${time}` : `Every ${names[Number(weekday)]} at ${time}`
    }
  }
  return expression
}
