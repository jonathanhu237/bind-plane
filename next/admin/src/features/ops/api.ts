import type { Vendor } from '@/features/release/asset-editors'

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

export type TaskKind = 'config_backup' | 'reachability'

export type OpsSwitch = {
  id: string; name: string; management_ip: string; vendor: Vendor; model: string | null; site: string | null; description: string | null
  group_id: string | null; group_name: string | null; protocol: 'telnet' | 'ssh'; port: number | null; is_enabled: boolean; version: number
  credential_id: string; credential_name: string; command_profile_id: string; profile_name: string
  reachable: boolean | null; reachability_checked_at: string | null; reachability_latency_ms: number | null; reachability_error: string | null
  config_checked_at: string | null; config_error: string | null; latest_backup_id: number | null; latest_backup_at: string | null; compliance_failed: number
}
export type Page<T> = { items: T[]; total: number; page_size: number }

export type TaskSummary = {
  id: string; kind: TaskKind; scope_label: string; actor_email: string | null; schedule_name: string | null; created_at: string
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
  id: number; kind: 'device_unreachable' | 'backup_failed' | 'compliance_violation'; severity: Severity; switch_id: string; switch_name: string
  management_ip: string; rule_id: string | null; rule_name: string | null; message: string; status: 'open' | 'acknowledged' | 'resolved'
  opened_at: string; last_seen_at: string; acknowledged_at: string | null; acknowledged_by: string | null; resolved_at: string | null
}
export type AuditEvent = { id: number; actor_email: string; action: string; object_id: string; object_name: string; changes: Record<string, unknown>; created_at: string }

export type Overview = {
  switches: { total: number; enabled: number; reachable: number; unreachable: number; unknown: number }
  backups: { backed_up: number; failing: number; never: number; versions_24h: number }
  compliance: { passed: number; failed: number; failing_switches: number }
  alerts: { severity: Severity; count: number }[]
  recent_tasks: TaskSummary[]
  recent_changes: { id: number; switch_id: string; switch_name: string; management_ip: string; captured_at: string; line_count: number }[]
  upcoming: { id: string; name: string; kind: TaskKind; next_run_at: string }[]
}

export type BackupMeta = { id: number; captured_at: string; size_bytes: number; line_count: number; sha256: string }
export type SwitchDetail = Omit<OpsSwitch, 'group_id' | 'credential_id' | 'command_profile_id' | 'version' | 'latest_backup_id' | 'latest_backup_at' | 'compliance_failed'> & {
  reachability_changed_at: string | null
  backups: BackupMeta[]
  compliance: { rule_id: string; rule_name: string; severity: Severity; mode: string; passed: boolean; detail: string | null; evaluated_at: string; backup_id: number }[]
  recent_items: { id: number; kind: TaskKind; status: TaskItem['status']; message: string | null; changed: boolean | null; backup_id: number | null; created_at: string; finished_at: string | null }[]
  alerts: { id: number; kind: Alert['kind']; severity: Severity; message: string; status: Alert['status']; opened_at: string }[]
  networks: { id: string; cidr: string; vlan: string | null; is_enabled: boolean; is_validated: boolean }[]
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
