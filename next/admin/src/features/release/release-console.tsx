import { useQuery } from '@tanstack/react-query'
import { ChevronRight, CircleAlert, Lock, TriangleAlert } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Empty, PageHeader, Section, ToneBadge, formatTime } from '@/features/ops/ui'
import type { User } from '@/shared/api/contracts'
import { serverText } from './messages'

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
const reasons = ['temporary_test', 'user_report', 'ip_mac_change', 'wrong_binding_fix', 'security_response', 'other'] as const
const reasonWords: Record<(typeof reasons)[number], Words> = {
  temporary_test: ['Temporary test', '临时测试'], user_report: ['User report', '用户反馈'], ip_mac_change: ['IP / MAC change', 'IP / MAC 变更'],
  wrong_binding_fix: ['Wrong binding fix', '纠正错误绑定'], security_response: ['Security response', '安全处置'], other: ['Other', '其他'],
}
const rawOutputs: Record<string, Words> = {
  raw_before_output: ['Raw output before release', '释放前原始输出'], raw_release_output: ['Raw release command output', '释放命令原始输出'], raw_after_output: ['Raw output after release', '释放后原始输出'],
}
// A query that stops before any job exists explains what to do next.
const stops: Record<string, Words> = { stopped_no_switch: noSwitch, stopped_ambiguous_switch: ambiguousSwitch }
// "Resolve the switch automatically" travels as this select value and is sent as no selection.
const AUTO = 'auto'

export const statusLabel = (status: string, zh: boolean) => say(statuses[status], zh, status.replaceAll('_', ' '))
export const phaseLabel = (phase: string, zh: boolean) => say(phases[phase], zh, phase.replaceAll('_', ' '))
export const entryLabel = (entry: string | undefined, zh: boolean) => (entry ? say(entryTypes[entry], zh, entry) : '—')
export const reasonLabel = (reason: string, zh: boolean) => say(reasonWords[reason as (typeof reasons)[number]], zh, reason.replaceAll('_', ' '))

const statusTone = (status: string) =>
  status === 'succeeded' ? 'good' : status === 'failed' || status === 'timeout' ? 'bad' : status === 'needs_manual_confirmation' ? 'warn' : 'muted'

function SwitchName({ job }: { job: Pick<Job, 'switch_id' | 'switch_name' | 'switch_management_ip'> }) {
  if (!job.switch_name) return <span className="break-all font-mono">{job.switch_id || '—'}</span>
  return <>{job.switch_name}{job.switch_management_ip ? <span className="ml-2 font-mono text-muted-foreground">{job.switch_management_ip}</span> : null}</>
}

function Info({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return <div className="min-w-0"><dt className="text-muted-foreground">{label}</dt><dd className={className}>{children}</dd></div>
}

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
  const assetsQuery = useQuery({
    queryKey: ['release-switch-options', user.id], enabled: admin,
    queryFn: () => request<{ switches: { id: string; name: string; is_enabled: boolean }[]; profiles: { id: string; name: string }[] }>('/api/releases/admin/assets'),
  })
  const preparationQuery = useQuery({ queryKey: ['release-preparation', user.id, jobID], queryFn: () => request<Job>(`/api/releases/jobs/${jobID}`), enabled: !!jobID,
    refetchInterval: (query) => query.state.data?.status === 'queued' || query.state.data?.status === 'running' ? 1800 : false })
  const resultQuery = useQuery({ queryKey: ['release-result', user.id, resultID], queryFn: () => request<Job>(`/api/releases/jobs/${resultID}`), enabled: !!resultID,
    refetchInterval: (query) => query.state.data?.status === 'queued' || query.state.data?.status === 'running' ? 1800 : false })
  const historyQuery = useQuery({ queryKey: ['release-history', user.id], queryFn: () => request<Job[]>('/api/releases/jobs'), enabled: authorized })
  // A query stays "waiting for confirmation" after its release starts; history shows it as confirmed.
  const confirmedQueries = new Set(historyQuery.data?.flatMap((job) => job.kind === 'release' && job.preparation_job_id ? [job.preparation_job_id] : []))
  const observed = preparationQuery.data
  const entry = observed?.before_state?.entry_type
  const stoppedNoRecord = observed?.status === 'cancelled' && entry === 'missing'
  const ready = observed?.status === 'waiting_confirmation' && entry === 'static' && !force || admin && entry === 'missing' && force && (observed?.status === 'waiting_confirmation' || stoppedNoRecord)
  useEffect(() => { if (observed?.status === 'waiting_confirmation' || observed?.status === 'cancelled') setForce(entry === 'missing' && Boolean(preparation?.force)) }, [observed?.status, entry, preparation?.force])
  const labels = zh ? {
    title: '静态绑定释放', intro: '输入一个 IPv4 地址。系统先定位交换机并查询绑定，确认后才会提交释放任务。', ip: 'IPv4 地址',
    reason: '释放原因', prepare: '查询绑定状态', preparing: '正在查询交换机…', confirm: '确认释放静态绑定',
    force: '管理员强制释放（查询无记录）', forceSwitch: '执行强制释放的交换机', auto: '自动解析交换机', empty: '暂无释放记录。', history: '最近任务', switch: '交换机',
    noAccess: '需要操作员或管理员角色才能使用释放控制台。', state: '当前状态', entryType: '绑定类型', kind: '类型', status: '状态', actions: '操作',
    query: '查询', release: '释放', retry: '重新查询', actor: '执行人', profile: '命令模板', finalState: '最终状态', forced: '强制释放', yes: '是', no: '否',
    retryOf: '重试自', original: '查看原任务', events: '执行过程',
    uncertain: '结果不确定。同一 IP 的后续释放已暂停；管理员必须先核实原执行器已停止及交换机现状，不要重复发出释放命令。',
  } : {
    title: 'Static binding release', intro: 'Enter one IPv4 address. The switch and binding are queried before confirmation.', ip: 'IPv4 address',
    reason: 'Reason', prepare: 'Query binding', preparing: 'Querying switch…', confirm: 'Confirm static binding release',
    force: 'Admin force release (no record found)', forceSwitch: 'Switch to force the release on', auto: 'Resolve the switch automatically', empty: 'No release jobs yet.', history: 'Recent jobs', switch: 'Switch',
    noAccess: 'An Operator or Admin role is required to use the release console.', state: 'Current state', entryType: 'Entry type', kind: 'Type', status: 'Status', actions: 'Actions',
    query: 'Query', release: 'Release', retry: 'Retry query', actor: 'Actor', profile: 'Command profile', finalState: 'Final state', forced: 'Forced', yes: 'Yes', no: 'No',
    retryOf: 'Retry of', original: 'View the original job', events: 'Progress',
    uncertain: 'Outcome uncertain. Further releases for this IP are blocked. An admin must verify the old executor has stopped and inspect the switch before any new command.',
  }
  const errorText = (cause: unknown) => releaseErrorText(cause, zh)
  const profileName = (id?: string) => (id ? assetsQuery.data?.profiles?.find((profile) => profile.id === id)?.name ?? id : '—')

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

  const result = resultQuery.data
  const forceToggle = (id: string) =>
    <Field orientation="horizontal">
      <Checkbox id={id} checked={force} onCheckedChange={(checked) => setForce(checked === true)} />
      <FieldLabel htmlFor={id} className="font-normal">{labels.force}</FieldLabel>
    </Field>
  return <div className="mx-auto flex w-full max-w-5xl flex-col gap-8">
    <PageHeader title={labels.title} description={labels.intro} />
    {!authorized ? <Alert><Lock aria-hidden="true" /><AlertDescription>{labels.noAccess}</AlertDescription></Alert> : <>
      <Card>
        <CardContent className="pt-6">
          <form noValidate onSubmit={(event) => void prepare(event)} className="flex flex-col gap-4">
            <div className="grid gap-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
              <Field data-invalid={Boolean(ipProblem)}>
                <FieldLabel htmlFor="target-ip">{labels.ip}</FieldLabel>
                <Input id="target-ip" ref={ipInput} inputMode="decimal" autoComplete="off" placeholder="10.0.0.42" className="font-mono" value={ip}
                  onChange={(event) => { setIP(event.target.value); setIPProblem('') }} aria-invalid={ipProblem ? true : undefined} aria-describedby={ipProblem ? 'target-ip-problem' : undefined} />
              </Field>
              {/* Under the address on phones; on wider screens a row of its own, so the bottom-aligned row keeps its line. */}
              {ipProblem ? <FieldError id="target-ip-problem" className="sm:order-last sm:col-span-3 sm:-mt-2">{ipProblem}</FieldError> : null}
              <Field>
                <FieldLabel htmlFor="reason">{labels.reason}</FieldLabel>
                <Select value={reason} onValueChange={setReason}>
                  <SelectTrigger id="reason"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>{reasons.map((value) => <SelectItem key={value} value={value}>{reasonLabel(value, zh)}</SelectItem>)}</SelectGroup></SelectContent>
                </Select>
              </Field>
              <Button type="submit" disabled={busy}>{busy ? labels.preparing : labels.prepare}</Button>
            </div>
            {admin ? <div className="flex flex-col gap-3">
              {forceToggle('force-prepare')}
              {force ? <Field className="max-w-sm">
                <FieldLabel htmlFor="force-switch">{labels.forceSwitch}</FieldLabel>
                <Select value={selectedSwitchID || AUTO} onValueChange={(value) => setSelectedSwitchID(value === AUTO ? '' : value)}>
                  <SelectTrigger id="force-switch"><SelectValue /></SelectTrigger>
                  <SelectContent><SelectGroup>
                    <SelectItem value={AUTO}>{labels.auto}</SelectItem>
                    {assetsQuery.data?.switches.filter((item) => item.is_enabled).map((item) => <SelectItem key={item.id} value={item.id}>{item.name}</SelectItem>)}
                  </SelectGroup></SelectContent>
                </Select>
              </Field> : null}
            </div> : null}
          </form>
        </CardContent>
      </Card>
      {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
      {preparation && !jobID ? stops[preparation.status]
        ? <Alert variant="warning"><TriangleAlert aria-hidden="true" /><AlertDescription>{say(stops[preparation.status], zh, '')}</AlertDescription></Alert>
        : <p role="status" className="text-sm text-muted-foreground">{statusLabel(preparation.status, zh)}</p> : null}
      {observed ? <Card aria-label={labels.state}>
        <CardHeader className="flex-row flex-wrap items-center justify-between gap-3">
          <CardTitle>{labels.state}</CardTitle>
          <Badge variant="secondary">{statusLabel(observed.status, zh)}</Badge>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <dl className="grid gap-3 text-sm sm:grid-cols-2">
            <Info label="IP" className="font-mono">{observed.target_ip}</Info>
            <Info label={labels.switch}><SwitchName job={observed} /></Info>
            <Info label="MAC" className="font-mono">{observed.before_state?.mac ?? '—'}</Info>
            <Info label={labels.reason}>{reasonLabel(observed.reason, zh)}</Info>
            <Info label={labels.entryType}>{entryLabel(entry, zh)}</Info>
          </dl>
          {(stoppedNoRecord || observed.status === 'waiting_confirmation' && entry === 'missing') && admin ? forceToggle('force-confirm') : null}
          {ready ? <Button type="button" className="self-start" disabled={busy} onClick={() => void confirm()}>{labels.confirm}</Button> : null}
        </CardContent>
      </Card> : null}
      {result ? <Card aria-live="polite">
        <CardHeader>
          <CardTitle>{statusLabel(result.status, zh)} · <span className="font-mono">{result.target_ip}</span></CardTitle>
          <CardDescription>{serverText(result.result?.message || result.error_message, zh) || phaseLabel(result.phase, zh)}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {result.status === 'needs_manual_confirmation' && result.kind === 'release' ? <Alert variant="warning"><TriangleAlert aria-hidden="true" /><AlertDescription>{labels.uncertain}</AlertDescription></Alert> : null}
          <dl className="grid gap-3 text-sm sm:grid-cols-2">
            <Info label={labels.actor}>{result.actor_email ?? '—'}</Info>
            <Info label={labels.switch}><SwitchName job={result} /></Info>
            <Info label={labels.reason}>{reasonLabel(result.reason, zh)}</Info>
            <Info label={labels.profile}>{profileName(result.command_profile_id)}</Info>
            <Info label={labels.finalState}>{entryLabel(result.after_state?.entry_type, zh)}</Info>
            <Info label={labels.forced}>{result.force ? labels.yes : labels.no}</Info>
            {result.retry_of_id ? <Info label={labels.retryOf}><Button type="button" variant="link" className="h-auto p-0" onClick={() => setResultID(result.retry_of_id ?? null)}>{labels.original}</Button></Info> : null}
          </dl>
          {result.events?.length ? <div className="flex flex-col gap-1">
            <h3 className="text-sm font-medium">{labels.events}</h3>
            <ol className="flex flex-col gap-1 text-xs text-muted-foreground">{result.events.map((event, index) => <li key={`${event.created_at}-${index}`}>
              <time dateTime={event.created_at} className="tabular-nums">{formatTime(event.created_at, zh)}</time> · {phaseLabel(event.phase, zh)} · {statusLabel(event.status, zh)} {serverText(event.message, zh)}
            </li>)}</ol>
          </div> : null}
          {(['raw_before_output', 'raw_release_output', 'raw_after_output'] as const).map((key) => result[key] ? <Collapsible key={key}>
            <CollapsibleTrigger asChild>
              <Button type="button" variant="ghost" size="sm" className="group -ml-3"><ChevronRight aria-hidden="true" data-icon="inline-start" className="transition-transform group-data-[state=open]:rotate-90" />{say(rawOutputs[key], zh, key)}</Button>
            </CollapsibleTrigger>
            <CollapsibleContent><pre className="mt-2 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">{result[key]}</pre></CollapsibleContent>
          </Collapsible> : null)}
        </CardContent>
      </Card> : null}
      <Section title={labels.history}>
        {historyQuery.data?.length ? <div className="overflow-x-auto rounded-xl border"><Table>
          <TableHeader><TableRow><TableHead>IP</TableHead><TableHead>{labels.switch}</TableHead><TableHead>{labels.kind}</TableHead><TableHead>{labels.status}</TableHead><TableHead className="text-right">{labels.actions}</TableHead></TableRow></TableHeader>
          <TableBody>{historyQuery.data.map((job) => {
            const status = job.kind === 'pre_release_query' && job.status === 'waiting_confirmation' && confirmedQueries.has(job.id) ? 'confirmed' : job.status
            return <TableRow key={job.id}>
              <TableCell><Button type="button" variant="link" className="h-auto p-0 font-mono" onClick={() => setResultID(job.id)}>{job.target_ip}</Button></TableCell>
              <TableCell className="text-muted-foreground">{job.switch_name ?? '—'}</TableCell>
              <TableCell>{job.kind === 'release' ? labels.release : labels.query}</TableCell>
              <TableCell><ToneBadge kind={statusTone(status)}>{statusLabel(status, zh)}</ToneBadge></TableCell>
              <TableCell className="text-right">{['failed', 'timeout', 'needs_manual_confirmation'].includes(job.status) && (!job.force || admin)
                ? <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void retry(job.id)}>{labels.retry}</Button> : null}</TableCell>
            </TableRow>
          })}</TableBody>
        </Table></div> : historyQuery.isPending ? null : <Empty>{labels.empty}</Empty>}
      </Section>
    </>}
  </div>
}
