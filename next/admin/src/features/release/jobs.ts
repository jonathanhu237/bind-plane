import type { User } from '@/shared/api/contracts'
import { serverText } from './messages'

export type Job = {
  id: string; job_id?: string; target_ip: string; switch_id: string; switch_name?: string; switch_management_ip?: string; command_profile_id?: string; status: string; phase: string; kind: string; reason: string; force: boolean
  before_state?: { entry_type?: string; mac?: string }; after_state?: { entry_type?: string }
  result?: { message?: string; preparation_status?: string }; error_message?: string
  raw_before_output?: string; raw_release_output?: string; raw_after_output?: string
  actor_email?: string; retry_of_id?: string; preparation_job_id?: string; release_job_id?: string; created_at?: string; started_at?: string; finished_at?: string
  events?: { phase: string; status: string; message?: string; created_at: string }[]
}
export type Preparation = { preparation_job_id?: string; status: string; target_ip: string; reason: string; force: boolean; resolved_switch?: { switch_id: string; network_id: string; cidr: string } }
export type JobPage = { items: Job[]; total: number; page_size: number }
export type JobFilter = { q?: string; kind?: string; status?: string; page?: number; pageSize?: number }
/** Switches and profiles from the admin asset list, for the force-release switch and profile names. */
export type AssetOptions = { switches: { id: string; name: string; is_enabled: boolean }[]; profiles: { id: string; name: string }[] }

export async function request<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST', credentials: 'same-origin', cache: 'no-store',
    headers: { Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data: unknown = await response.json()
  if (!response.ok) {
    const error = data as { detail?: string }
    throw new Error(error.detail || `HTTP ${response.status}`)
  }
  return data as T
}

/** The history endpoint for one page of jobs; empty filters are left out. */
export function jobsPath({ q, kind, status, page, pageSize }: JobFilter): string {
  const params = new URLSearchParams()
  if (q?.trim()) params.set('q', q.trim())
  if (kind) params.set('kind', kind)
  if (status) params.set('status', status)
  if (page && page > 1) params.set('page', String(page))
  if (pageSize) params.set('page_size', String(pageSize))
  const query = params.toString()
  return `/api/releases/jobs${query ? `?${query}` : ''}`
}

// Lists of jobs share this key prefix, so a confirm or retry refreshes all of them.
export const jobListKey = 'release-jobs'
export const assetOptionsKey = 'release-asset-options'

/** Operators and admins release bindings; any other role sees no release workflow. */
export const canRelease = (user: User) => Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin' || role.name === 'Operator'))

export const isRunning = (job: Pick<Job, 'status'> | undefined) => job?.status === 'queued' || job?.status === 'running'

/** A query stays "waiting for confirmation" after its release starts; it is shown as confirmed. */
export const shownStatus = (job: Pick<Job, 'kind' | 'status' | 'release_job_id'>) =>
  job.kind === 'pre_release_query' && job.status === 'waiting_confirmation' && job.release_job_id ? 'confirmed' : job.status

/** Failed, timed-out and uncertain jobs can start a fresh query; a forced one only by an admin. */
export const canRetry = (job: Pick<Job, 'status' | 'force'>, admin: boolean) =>
  ['failed', 'timeout', 'needs_manual_confirmation'].includes(job.status) && (!job.force || admin)

type Words = [english: string, chinese: string]
const say = (words: Words | undefined, zh: boolean, fallback: string) => (words ? words[zh ? 1 : 0] : fallback)

const noSwitch: Words = [
  'No enabled switch owns a validated network containing this address. An admin can check the networks under Asset settings.',
  '没有哪台已启用的交换机拥有包含该地址、且已验证的网段。管理员可以在“资产配置”中检查网段。',
]
const ambiguousSwitch: Words = [
  'Several switches own equally specific networks containing this address. An admin must correct the network data first.',
  '有多台交换机的网段同样精确地包含该地址，需要管理员先修正网段数据。',
]

// The confirmation the operator saw no longer describes the switch a release would reach.
const assetChanged = 'switch configuration changed since the pre-query; query again'

// Refusals whose meaning is specific to releases; everything else goes through serverText.
const releaseErrors: Record<string, Words> = {
  [assetChanged]: ['The switch configuration changed after this query. Query the binding again before releasing.', '查询之后交换机配置已变更。请重新查询绑定状态后再释放。'],
  'one IPv4 address is required': ['Enter one IPv4 address, for example 10.0.0.42.', '请输入一个 IPv4 地址，例如 10.0.0.42。'],
  'no enabled validated switch owns this IP': noSwitch,
  'multiple equally specific switches own this IP': ambiguousSwitch,
  conflict: [
    'This release cannot go ahead: the query is out of date, or another release of this address is still running or awaiting manual confirmation. Query again.',
    '这次释放无法继续：查询结果已过期，或同一地址的另一次释放仍在执行或等待人工确认。请重新查询。',
  ],
  'not found': ['This job no longer exists, or it belongs to someone else.', '任务不存在，或不属于你。'],
}

export function releaseErrorText(cause: unknown, zh: boolean): string {
  if (!(cause instanceof Error)) return zh ? '操作失败，请重试。' : 'Something went wrong. Try again.'
  return say(releaseErrors[cause.message], zh, serverText(cause.message, zh))
}

const ipv4 = /^((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/

/** What is wrong with the typed address, in the interface language; empty when it can be queried. */
export function targetIPProblem(value: string, zh: boolean): string {
  const ip = value.trim()
  if (!ip) return zh ? '请输入要查询的 IPv4 地址。' : 'Enter the IPv4 address to query.'
  if (ipv4.test(ip)) return ''
  return zh ? '这不是有效的 IPv4 地址，请按 10.0.0.42 这样的格式输入。' : 'That is not an IPv4 address. Enter one like 10.0.0.42.'
}

// Job states, phases, binding types and reasons arrive as codes; unknown codes are shown as sent.
const statuses: Record<string, Words> = {
  queued: ['Queued', '排队中'], query_queued: ['Query queued', '查询排队中'], running: ['Running', '执行中'],
  waiting_confirmation: ['Waiting for confirmation', '等待确认'], confirmed: ['Confirmed', '已确认'], succeeded: ['Succeeded', '成功'],
  failed: ['Failed', '失败'], timeout: ['Timed out', '超时'], cancelled: ['Stopped', '已停止'], needs_manual_confirmation: ['Needs manual confirmation', '需人工确认'],
  stopped_no_switch: ['No switch matched', '未匹配到交换机'], stopped_ambiguous_switch: ['Several switches matched', '匹配到多台交换机'],
}
const phases: Record<string, Words> = {
  queued: ['Queued', '排队中'], connecting: ['Connecting to the switch', '正在连接交换机'], querying_before: ['Querying before release', '正在查询释放前状态'],
  waiting_confirmation: ['Waiting for confirmation', '等待确认'], releasing: ['Releasing', '正在释放'], querying_after: ['Querying after release', '正在查询释放后状态'],
  classifying: ['Judging the result', '正在判定结果'], finished: ['Finished', '已结束'],
}
const entryTypes: Record<string, Words> = { static: ['Static binding', '静态绑定'], dynamic: ['Dynamic entry', '动态记录'], missing: ['No record', '无记录'], unknown: ['Unclear', '无法判断'] }
export const reasons = ['temporary_test', 'user_report', 'ip_mac_change', 'wrong_binding_fix', 'security_response', 'other'] as const
const reasonWords: Record<(typeof reasons)[number], Words> = {
  temporary_test: ['Temporary test', '临时测试'], user_report: ['User report', '用户反馈'], ip_mac_change: ['IP / MAC change', 'IP / MAC 变更'],
  wrong_binding_fix: ['Wrong binding fix', '纠正错误绑定'], security_response: ['Security response', '安全处置'], other: ['Other', '其他'],
}
export const rawOutputs = ['raw_before_output', 'raw_release_output', 'raw_after_output'] as const
const rawOutputWords: Record<(typeof rawOutputs)[number], Words> = {
  raw_before_output: ['Raw output before release', '释放前原始输出'], raw_release_output: ['Raw release command output', '释放命令原始输出'], raw_after_output: ['Raw output after release', '释放后原始输出'],
}
// A query that stops before any job exists explains what to do next.
const stops: Record<string, Words> = { stopped_no_switch: noSwitch, stopped_ambiguous_switch: ambiguousSwitch }

export const statusLabel = (status: string, zh: boolean) => say(statuses[status], zh, status.replaceAll('_', ' '))
export const phaseLabel = (phase: string, zh: boolean) => say(phases[phase], zh, phase.replaceAll('_', ' '))
export const entryLabel = (entry: string | undefined, zh: boolean) => (entry ? say(entryTypes[entry], zh, entry) : '—')
export const reasonLabel = (reason: string, zh: boolean) => say(reasonWords[reason as (typeof reasons)[number]], zh, reason.replaceAll('_', ' '))
export const rawOutputLabel = (key: (typeof rawOutputs)[number], zh: boolean) => say(rawOutputWords[key], zh, key)
export const jobKindLabel = (kind: string, zh: boolean) => (kind === 'release' ? (zh ? '释放' : 'Release') : zh ? '查询' : 'Query')
/** Why a query stopped before a job existed; empty for every other status. */
export const stopText = (status: string, zh: boolean) => say(stops[status], zh, '')

export const statusTone = (status: string) =>
  status === 'succeeded' ? 'good' : status === 'failed' || status === 'timeout' ? 'bad' : status === 'needs_manual_confirmation' ? 'warn' : 'muted'
