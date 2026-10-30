import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { FieldError } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import type { User } from '@/shared/api/contracts'

type Job = {
  id: string; job_id?: string; target_ip: string; switch_id: string; switch_name?: string; switch_management_ip?: string; command_profile_id?: string; status: string; phase: string; kind: string; reason: string; force: boolean
  before_state?: { entry_type?: string; mac?: string }; after_state?: { entry_type?: string }
  result?: { message?: string; preparation_status?: string }; error_message?: string
  raw_before_output?: string; raw_release_output?: string; raw_after_output?: string
  actor_email?: string; retry_of_id?: string; preparation_job_id?: string; created_at?: string; started_at?: string; finished_at?: string
  events?: { phase: string; status: string; message?: string; created_at: string }[]
}
type Preparation = { preparation_job_id?: string; status: string; target_ip: string; reason: string; force: boolean; resolved_switch?: { switch_id: string; network_id: string; cidr: string } }

// The confirmation the operator saw no longer describes the switch a release would reach.
const assetChanged = 'switch configuration changed since the pre-query; query again'

// Refusals the release API sends as plain English, in [English, Chinese].
const releaseErrors: Record<string, [string, string]> = {
  [assetChanged]: ['The switch configuration changed after this query. Query the binding again before releasing.', '查询之后交换机配置已变更。请重新查询绑定状态后再释放。'],
  'one IPv4 address is required': ['Enter one IPv4 address, for example 10.0.0.42.', '请输入一个 IPv4 地址，例如 10.0.0.42。'],
  'no enabled validated switch owns this IP': [
    'No enabled switch owns a validated network containing this address. An admin can check the networks under Asset settings.',
    '没有哪台已启用的交换机拥有包含该地址、且已验证的网段。管理员可以在“资产配置”中检查网段。',
  ],
  'multiple equally specific switches own this IP': [
    'Several switches own equally specific networks containing this address. An admin must correct the network data first.',
    '有多台交换机的网段同样精确地包含该地址，需要管理员先修正网段数据。',
  ],
  'release reason required': ['Choose a release reason.', '请选择释放原因。'],
  'admin force required': ['Only an admin can force a release.', '只有管理员可以强制释放。'],
  'operator role required': ['An Operator or Admin role is required.', '需要操作员或管理员角色。'],
  'login required': ['Your session has ended. Sign in again.', '登录已失效，请重新登录。'],
  conflict: [
    'This release cannot go ahead: the query is out of date, or another release of this address is still running or awaiting manual confirmation. Query again.',
    '这次释放无法继续：查询结果已过期，或同一地址的另一次释放仍在执行或等待人工确认。请重新查询。',
  ],
  'not found': ['This job no longer exists, or it belongs to someone else.', '任务不存在，或不属于你。'],
}

export function releaseErrorText(cause: unknown, zh: boolean): string {
  if (!(cause instanceof Error)) return zh ? '操作失败' : 'Request failed'
  const known = releaseErrors[cause.message]
  return known ? known[zh ? 1 : 0] : cause.message
}

const ipv4 = /^((25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)$/

/** What is wrong with the typed address, in the interface language; empty when it can be queried. */
export function targetIPProblem(value: string, zh: boolean): string {
  const ip = value.trim()
  if (!ip) return zh ? '请输入要查询的 IPv4 地址。' : 'Enter the IPv4 address to query.'
  if (ipv4.test(ip)) return ''
  return zh ? '这不是有效的 IPv4 地址，请按 10.0.0.42 这样的格式输入。' : 'That is not an IPv4 address. Enter one like 10.0.0.42.'
}

function SwitchName({ job }: { job: Pick<Job, 'switch_id' | 'switch_name' | 'switch_management_ip'> }) {
  if (!job.switch_name) return <span className="break-all font-mono">{job.switch_id || '—'}</span>
  return <>{job.switch_name}{job.switch_management_ip ? <span className="ml-2 font-mono text-muted-foreground">{job.switch_management_ip}</span> : null}</>
}

const reasons = ['temporary_test', 'user_report', 'ip_mac_change', 'wrong_binding_fix', 'security_response', 'other'] as const
const zhReasons = ['临时测试', '用户反馈', 'IP / MAC 变更', '纠正错误绑定', '安全处置', '其他']

async function request<T>(path: string, body?: unknown): Promise<T> {
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

export function ReleaseConsole({ user }: { user: User }) {
  const { i18n } = useTranslation()
  const zh = i18n.language.startsWith('zh')
  const admin = Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin'))
  const authorized = admin || Boolean(user.roles?.some((role) => role.name === 'Operator'))
  const [ip, setIP] = useState('')
  const [ipProblem, setIPProblem] = useState('')
  const ipInput = useRef<HTMLInputElement>(null)
  const [reason, setReason] = useState<string>('temporary_test')
  const [preparation, setPreparation] = useState<Preparation | null>(null)
  const [jobID, setJobID] = useState<string | null>(null)
  const [resultID, setResultID] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [force, setForce] = useState(false)
  const [selectedSwitchID, setSelectedSwitchID] = useState('')
  const assetsQuery = useQuery({ queryKey: ['release-switch-options', user.id], queryFn: () => request<{ switches: { id: string; name: string; is_enabled: boolean }[] }>('/api/releases/admin/assets'), enabled: admin })
  const preparationQuery = useQuery({ queryKey: ['release-preparation', user.id, jobID], queryFn: () => request<Job>(`/api/releases/jobs/${jobID}`), enabled: !!jobID,
    refetchInterval: (query) => query.state.data?.status === 'queued' || query.state.data?.status === 'running' ? 1800 : false })
  const resultQuery = useQuery({ queryKey: ['release-result', user.id, resultID], queryFn: () => request<Job>(`/api/releases/jobs/${resultID}`), enabled: !!resultID,
    refetchInterval: (query) => query.state.data?.status === 'queued' || query.state.data?.status === 'running' ? 1800 : false })
  const historyQuery = useQuery({ queryKey: ['release-history', user.id], queryFn: () => request<Job[]>('/api/releases/jobs'), enabled: authorized })
  const observed = preparationQuery.data
  const entry = observed?.before_state?.entry_type
  const stoppedNoRecord = observed?.status === 'cancelled' && entry === 'missing'
  const ready = observed?.status === 'waiting_confirmation' && entry === 'static' && !force || admin && entry === 'missing' && force && (observed?.status === 'waiting_confirmation' || stoppedNoRecord)
  useEffect(() => { if (observed?.status === 'waiting_confirmation' || observed?.status === 'cancelled') setForce(entry === 'missing' && Boolean(preparation?.force)) }, [observed?.status, entry, preparation?.force])
  const labels = zh ? {
    title: '静态绑定释放', intro: '输入一个 IPv4 地址。系统先定位交换机并查询绑定，确认后才会提交释放任务。', ip: 'IPv4 地址',
    reason: '释放原因', prepare: '查询绑定状态', preparing: '正在查询交换机…', confirm: '确认释放静态绑定',
    force: '管理员强制释放（查询无记录）', empty: '暂无释放记录', history: '最近任务', switch: '交换机',
    noAccess: '需要操作员或管理员角色才能使用释放控制台。', state: '当前状态',
  } : {
    title: 'Static binding release', intro: 'Enter one IPv4 address. The switch and binding are queried before confirmation.', ip: 'IPv4 address',
    reason: 'Reason', prepare: 'Query binding', preparing: 'Querying switch…', confirm: 'Confirm static binding release',
    force: 'Admin force release (no record found)', empty: 'No release jobs yet', history: 'Recent jobs', switch: 'Switch',
    noAccess: 'An Operator or Admin role is required to use the release console.', state: 'Current state',
  }
  const errorText = (cause: unknown) => releaseErrorText(cause, zh)

  async function prepare(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const problem = targetIPProblem(ip, zh)
    setIPProblem(problem)
    if (problem) { ipInput.current?.focus(); return }
    setBusy(true); setError(''); setPreparation(null); setJobID(null); setResultID(null); setForce(false)
    try {
      const data = await request<Preparation>('/api/releases/prepare', { target_ip: ip.trim(), reason, force: admin && force, ...(admin && force && selectedSwitchID ? { selected_switch_id: selectedSwitchID } : {}) })
      setPreparation(data); setJobID(data.preparation_job_id ?? null)
    } catch (cause) { setError(errorText(cause)) }
    finally { setBusy(false) }
  }
  async function retry(id: string) {
    setBusy(true); setError(''); setResultID(null)
    try {
      const next = await request<Job>(`/api/releases/jobs/${id}/retry`, {})
      setPreparation({ preparation_job_id: next.job_id || next.id, status: 'query_queued', target_ip: next.target_ip, reason: next.reason, force: next.force, resolved_switch: { switch_id: next.switch_id, network_id: '', cidr: '' } })
      setJobID(next.job_id || next.id); setIP(next.target_ip); setIPProblem(''); setReason(next.reason); setForce(false)
      await historyQuery.refetch()
    } catch (cause) { setError(errorText(cause)) }
    finally { setBusy(false) }
  }
  async function confirm() {
    if (!preparation?.preparation_job_id) return
    setBusy(true); setError('')
    try {
      const data = await request<{ job_id: string }>('/api/releases', { preparation_job_id: preparation.preparation_job_id, force })
      setResultID(data.job_id); await historyQuery.refetch()
    } catch (cause) { setError(errorText(cause)) }
    finally { setBusy(false) }
  }

  return <div className="mx-auto flex w-full max-w-5xl flex-col gap-8">
    <header className="space-y-2"><h1 className="text-2xl font-semibold tracking-tight">{labels.title}</h1><p className="text-sm text-muted-foreground">{labels.intro}</p></header>
    {!authorized ? <p role="alert" className="text-sm text-destructive">{labels.noAccess}</p> : <>
      <form noValidate onSubmit={(event) => void prepare(event)} className="grid gap-4 rounded-xl border bg-card p-5 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
        <div className="space-y-2"><Label htmlFor="target-ip">{labels.ip}</Label><Input id="target-ip" ref={ipInput} inputMode="decimal" autoComplete="off" placeholder="10.0.0.42" value={ip} onChange={(event) => { setIP(event.target.value); setIPProblem('') }} required aria-invalid={ipProblem ? true : undefined} aria-describedby={ipProblem ? 'target-ip-problem' : undefined} /></div>
        {/* Under the address on phones; on wider screens a full row under the inputs, so the bottom-aligned row keeps its line. */}
        {ipProblem ? <FieldError id="target-ip-problem" className="sm:order-1 sm:col-span-3 sm:-mt-2">{ipProblem}</FieldError> : null}
        <div className="space-y-2"><Label htmlFor="reason">{labels.reason}</Label><select id="reason" value={reason} onChange={(event) => setReason(event.target.value)} className="flex h-9 w-full rounded-md border border-input bg-transparent px-3 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
          {reasons.map((value, index) => <option key={value} value={value}>{zh ? zhReasons[index] : value.replaceAll('_', ' ')}</option>)}
        </select></div>
        <Button type="submit" disabled={busy}>{busy ? labels.preparing : labels.prepare}</Button>
        {admin ? <div className="flex flex-col gap-2 sm:order-2 sm:col-span-3"><label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} />{labels.force}</label>{force ? <select aria-label={labels.switch} value={selectedSwitchID} onChange={(event) => setSelectedSwitchID(event.target.value)} className="w-full max-w-sm rounded-md border bg-background px-3 py-2 text-sm"><option value="">{zh ? '自动解析交换机' : 'Resolve switch automatically'}</option>{assetsQuery.data?.switches.filter((item) => item.is_enabled).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select> : null}</div> : null}
      </form>
      {error ? <p role="alert" className="rounded-md border border-destructive/40 p-3 text-sm text-destructive">{error}</p> : null}
      {preparation && !jobID ? <p role="status" className="text-sm text-muted-foreground">{preparation.status.replaceAll('_', ' ')}</p> : null}
      {observed ? <section aria-label={labels.state} className="space-y-4 rounded-xl border bg-card p-5">
        <div className="flex flex-wrap items-center justify-between gap-3"><h2 className="text-lg font-medium">{labels.state}</h2><span className="rounded-full bg-muted px-3 py-1 text-xs font-medium">{observed.status.replaceAll('_', ' ')}</span></div>
        <dl className="grid gap-3 text-sm sm:grid-cols-2"><div><dt className="text-muted-foreground">IP</dt><dd className="font-mono">{observed.target_ip}</dd></div><div><dt className="text-muted-foreground">{labels.switch}</dt><dd><SwitchName job={observed} /></dd></div><div><dt className="text-muted-foreground">MAC</dt><dd className="font-mono">{observed.before_state?.mac ?? '—'}</dd></div><div><dt className="text-muted-foreground">{labels.reason}</dt><dd>{observed.reason.replaceAll('_', ' ')}</dd></div><div><dt className="text-muted-foreground">{zh ? '绑定类型' : 'Entry type'}</dt><dd>{entry ?? '—'}</dd></div></dl>
        {(stoppedNoRecord || observed.status === 'waiting_confirmation' && entry === 'missing') && admin ? <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={force} onChange={(event) => setForce(event.target.checked)} />{labels.force}</label> : null}
        {ready ? <Button type="button" disabled={busy} onClick={() => void confirm()}>{labels.confirm}</Button> : null}
      </section> : null}
      {resultQuery.data ? <section aria-live="polite" className="space-y-4 rounded-xl border bg-card p-5"><h2 className="text-lg font-medium">{resultQuery.data.status.replaceAll('_', ' ')} · {resultQuery.data.target_ip}</h2><p className="text-sm text-muted-foreground">{resultQuery.data.result?.message || resultQuery.data.error_message || resultQuery.data.phase}</p>{resultQuery.data.status === 'needs_manual_confirmation' && resultQuery.data.kind === 'release' ? <p role="alert" className="rounded-md border border-amber-400/50 p-3 text-sm">{zh ? '结果不确定。同一 IP 的后续释放已暂停；管理员必须先核实原执行器已停止及交换机现状，不要重复发出释放命令。' : 'Outcome uncertain. Further releases for this IP are blocked. An admin must verify the old executor has stopped and inspect the switch before any new command.'}</p> : null}<dl className="grid gap-2 text-sm sm:grid-cols-2"><div><dt className="text-muted-foreground">{zh ? '执行人' : 'Actor'}</dt><dd>{resultQuery.data.actor_email ?? '—'}</dd></div><div><dt className="text-muted-foreground">{zh ? '交换机' : 'Switch'}</dt><dd><SwitchName job={resultQuery.data} /></dd></div><div><dt className="text-muted-foreground">{zh ? '原因' : 'Reason'}</dt><dd>{resultQuery.data.reason.replaceAll('_', ' ')}</dd></div><div><dt className="text-muted-foreground">{zh ? '命令配置' : 'Command profile'}</dt><dd className="break-all font-mono">{resultQuery.data.command_profile_id ?? '—'}</dd></div><div><dt className="text-muted-foreground">{zh ? '最终状态' : 'Final state'}</dt><dd>{resultQuery.data.after_state?.entry_type ?? '—'}</dd></div><div><dt className="text-muted-foreground">{zh ? '强制释放' : 'Forced'}</dt><dd>{resultQuery.data.force ? (zh ? '是' : 'Yes') : (zh ? '否' : 'No')}</dd></div><div><dt className="text-muted-foreground">{zh ? '关联重试' : 'Retry of'}</dt><dd className="break-all font-mono">{resultQuery.data.retry_of_id ?? '—'}</dd></div></dl><ol className="space-y-1 text-xs text-muted-foreground">{resultQuery.data.events?.map((event, index) => <li key={`${event.created_at}-${index}`}><time>{new Date(event.created_at).toLocaleString()}</time> · {event.phase.replaceAll('_', ' ')} · {event.status.replaceAll('_', ' ')} {event.message ?? ''}</li>)}</ol>{(['raw_before_output', 'raw_release_output', 'raw_after_output'] as const).map((key) => resultQuery.data?.[key] ? <details key={key} className="text-sm"><summary className="cursor-pointer">{key.replaceAll('_', ' ')}</summary><pre className="mt-2 overflow-x-auto rounded-md bg-muted p-3 text-xs">{resultQuery.data[key]}</pre></details> : null)}</section> : null}
      <section className="space-y-3"><h2 className="text-lg font-medium">{labels.history}</h2>{historyQuery.data?.length ? <ul className="divide-y rounded-xl border">{historyQuery.data.map((job) => <li key={job.id} className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 text-sm"><span className="flex min-w-0 items-baseline gap-3"><button type="button" className="font-mono text-left underline-offset-4 hover:underline focus-visible:underline" onClick={() => setResultID(job.id)}>{job.target_ip}</button>{job.switch_name ? <span className="truncate text-muted-foreground">{job.switch_name}</span> : null}</span><span className="flex items-center gap-3">{job.status.replaceAll('_', ' ')}{['failed', 'timeout', 'needs_manual_confirmation'].includes(job.status) && (!job.force || admin) ? <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void retry(job.id)}>{zh ? '重新查询' : 'Retry query'}</Button> : null}</span></li>)}</ul> : <p className="text-sm text-muted-foreground">{labels.empty}</p>}</section>
    </>}
  </div>
}
