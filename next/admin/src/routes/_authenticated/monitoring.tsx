import { createFileRoute, Link, useLoaderData } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, Settings2, TriangleAlert } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { OpsRequestError, opsRequest, type MonitoredSwitch, type MonitoringPage, type MonitoringSettings, type TaskKind } from '@/features/ops/api'
import { DeviceDetailSheet } from '@/features/ops/device-detail'
import { FacetFilter } from '@/features/ops/facet-filter'
import { formatUptime } from '@/features/ops/monitoring'
import { AdminOnly, Empty, ErrorNote, PageHeader, Pager, ToneBadge, Time, isAdmin, kindLabel, pick, useZh } from '@/features/ops/ui'
import { NO_GROUP, callAssets, type Inventory } from '@/features/release/asset-editors'
import { errorText, serverText } from '@/features/release/messages'
import { cn } from '@/lib/utils'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

type MonitoringSearch = { q?: string; group?: string; state?: string; sort?: string; page?: number }
const STATES = ['no_response', 'high', 'waiting', 'restarted', 'unmonitored'] as const
const SORTS = ['cpu', 'memory', 'temperature', 'uptime', 'response'] as const

export const Route = createFileRoute('/_authenticated/monitoring')({
  validateSearch: (search: Record<string, unknown>): MonitoringSearch => {
    const text = (key: string) => (typeof search[key] === 'string' && search[key] ? (search[key] as string) : undefined)
    const page = Number(search.page)
    return {
      q: text('q'), group: text('group'),
      state: STATES.find((state) => state === search.state), sort: SORTS.find((sort) => sort === search.sort),
      page: Number.isInteger(page) && page > 1 ? page : undefined,
    }
  },
  component: MonitoringRoute,
})

function MonitoringRoute() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  if (!isAdmin(user)) return <AdminOnly zh={zh} />
  return <MonitoringView />
}

function MonitoringView() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const { t } = useTranslation()
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const queryClient = useQueryClient()
  const [text, setText] = useState(search.q ?? '')
  const [detail, setDetail] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [editingSettings, setEditingSettings] = useState(false)
  const params = new URLSearchParams()
  for (const key of ['q', 'group', 'state', 'sort'] as const) if (search[key]) params.set(key, search[key])
  params.set('page', String(search.page ?? 1))
  const listKey = params.toString()
  const list = useQuery({ queryKey: ['ops-monitoring', listKey], queryFn: () => opsRequest<MonitoringPage>(`monitoring?${listKey}`), placeholderData: keepPreviousData, refetchInterval: 15_000 })
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), staleTime: 60_000 })
  const filter = (patch: MonitoringSearch) => void navigate({ search: (previous) => ({ ...previous, ...patch, page: undefined }), replace: true })
  useEffect(() => {
    const timer = setTimeout(() => { if ((search.q ?? '') !== text.trim()) filter({ q: text.trim() || undefined }) }, 300)
    return () => clearTimeout(timer)
  })
  const data = list.data
  const summary = data?.summary
  const settings = data?.settings
  async function run(kind: TaskKind, id: string) {
    setBusy(true)
    try {
      await opsRequest('tasks', { kind, scope: 'switches', switch_ids: [id] })
      notifySuccess(pick(zh, `${kindLabel(kind, zh)}已排队。`, `${kindLabel(kind, zh)} queued.`))
    } catch (cause) { notifyRequestError(cause, t, { description: errorText(cause, zh) }) }
    finally { setBusy(false) }
  }
  const stateNames: Record<(typeof STATES)[number], string> = {
    no_response: pick(zh, '无响应', 'Not responding'), high: pick(zh, '超过阈值', 'Over a threshold'), waiting: pick(zh, '等待首次采集', 'Not polled yet'),
    restarted: pick(zh, '24 小时内重启', 'Restarted in 24 hours'), unmonitored: pick(zh, '未监控', 'Not monitored'),
  }
  const sortNames: Record<(typeof SORTS)[number], string> = {
    cpu: pick(zh, 'CPU 最高', 'Highest CPU'), memory: pick(zh, '内存最高', 'Highest memory'), temperature: pick(zh, '温度最高', 'Hottest'),
    uptime: pick(zh, '运行时长最短', 'Shortest uptime'), response: pick(zh, '响应最慢', 'Slowest answer'),
  }
  const stat = (state: (typeof STATES)[number] | undefined, label: string, value: number | undefined, tone: 'bad' | 'warn' | 'muted' = 'muted') =>
    <button type="button" onClick={() => filter({ state })} aria-pressed={search.state === state}
      className={cn('flex min-w-0 flex-col items-start gap-0.5 rounded-xl border px-4 py-3 text-left transition-colors hover:bg-muted/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring',
        search.state === state && 'border-foreground/30 bg-muted/50')}>
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className={cn('text-2xl font-semibold', value && tone === 'bad' && 'text-destructive', value && tone === 'warn' && 'text-warning')}>{value ?? '—'}</span>
    </button>

  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '设备监控', 'Monitoring')}>
      <Button type="button" variant="outline" disabled={!settings} onClick={() => setEditingSettings(true)}><Settings2 aria-hidden="true" data-icon="inline-start" />{pick(zh, '监控设置', 'Settings')}</Button>
    </PageHeader>
    <ErrorNote error={list.error} zh={zh} />
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
      {stat(undefined, pick(zh, '监控中', 'Monitored'), summary?.monitored)}
      {stat('no_response', pick(zh, '无响应', 'Not responding'), summary?.no_response, 'bad')}
      {stat('high', pick(zh, '超过阈值', 'Over a threshold'), summary?.high, 'warn')}
      {stat('restarted', pick(zh, '24 小时内重启', 'Restarted in 24 h'), summary?.restarted, 'warn')}
      {stat('unmonitored', pick(zh, '未监控', 'Not monitored'), summary?.unmonitored)}
    </div>
    {summary && !summary.monitored ? <Alert>
      <CircleAlert aria-hidden="true" />
      <AlertDescription>{pick(zh, '还没有监控任何交换机。先在“凭据”页新建 SNMP 凭据，再到设备页选中交换机，用“SNMP 监控…”开始监控。', 'No switch is monitored yet. Create an SNMP credential on the Credentials page, then select switches on the Devices page and start monitoring them under “Monitoring…”.')}{' '}
        <Link to="/credentials" className="underline underline-offset-4">{pick(zh, '去凭据页', 'Go to credentials')}</Link></AlertDescription>
    </Alert> : null}
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={pick(zh, '搜索名称、IP 或系统名称', 'Search name, IP or system name')} placeholder={pick(zh, '搜索名称、IP 或系统名称', 'Search name, IP or system name')} value={text}
        onChange={(event) => setText(event.target.value)} className="h-9 w-full sm:w-64" autoComplete="off" />
      <FacetFilter label={pick(zh, '分组', 'Group')} value={search.group} anyLabel={pick(zh, '全部分组', 'All groups')} onChange={(group) => filter({ group })}
        options={[...(inventory.data?.groups ?? []).map((group) => [group.id, group.name] as [string, string]), [NO_GROUP, pick(zh, '未分组', 'No group')]]} />
      <FacetFilter label={pick(zh, '状态', 'State')} value={search.state} anyLabel={pick(zh, '全部监控中', 'All monitored')} onChange={(state) => filter({ state })}
        options={STATES.map((state) => [state, stateNames[state]])} />
      <FacetFilter label={pick(zh, '排序', 'Sort')} value={search.sort} anyLabel={pick(zh, '按名称', 'By name')} onChange={(sort) => filter({ sort })}
        options={SORTS.map((sort) => [sort, sortNames[sort]])} />
    </div>
    {data && !data.items.length
      ? <Empty>{search.state === 'unmonitored' ? pick(zh, '启用的交换机都在监控中。', 'Every enabled switch is monitored.') : pick(zh, '没有符合条件的交换机。', 'No switch matches.')}</Empty>
      : <div className={cn('overflow-x-auto rounded-xl border transition-opacity [&_th]:whitespace-nowrap', list.isPlaceholderData && 'opacity-60')}>
        <Table>
          <TableHeader><TableRow>
            <TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '状态', 'State')}</TableHead>
            <TableHead className="text-right">CPU</TableHead><TableHead className="text-right">{pick(zh, '内存', 'Memory')}</TableHead><TableHead className="text-right">{pick(zh, '温度', 'Temp.')}</TableHead>
            <TableHead>{pick(zh, '运行时长', 'Uptime')}</TableHead><TableHead className="text-right">{pick(zh, '端口', 'Ports')}</TableHead>
            <TableHead className="text-right">{pick(zh, '响应', 'Answer')}</TableHead><TableHead>{pick(zh, '最近采集', 'Polled')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>{data?.items.map((row) => <MonitoringRow key={row.id} row={row} settings={settings} zh={zh} onOpen={() => setDetail(row.id)} />)}</TableBody>
        </Table>
      </div>}
    {data && data.total ? <Pager page={search.page ?? 1} total={data.total} pageSize={data.page_size} zh={zh} onPage={(page) => void navigate({ search: (previous) => ({ ...previous, page: page > 1 ? page : undefined }) })} /> : null}
    <p className="text-xs text-muted-foreground">{settings
      ? pick(zh, `每 ${settings.interval_minutes} 分钟通过 SNMP 采集一次，只读取不修改。连续 ${settings.consecutive_polls} 次超过阈值或无响应时产生告警。`,
        `Switches are polled over SNMP every ${settings.interval_minutes} minute${settings.interval_minutes === 1 ? '' : 's'}; polls only read. ${settings.consecutive_polls} polls in a row over a threshold, or without an answer, raise an alert.`)
      : null}</p>
    {settings ? <SettingsDialog key={settings.version} open={editingSettings} settings={settings} zh={zh} onClose={() => setEditingSettings(false)}
      onSaved={async () => { setEditingSettings(false); await list.refetch(); await queryClient.invalidateQueries({ queryKey: ['ops-switch'] }) }}
      onStale={() => void list.refetch()} /> : null}
    <DeviceDetailSheet switchId={detail} zh={zh} busy={busy} inventory={inventory.data} tab="monitoring" onClose={() => setDetail(null)} onRun={(kind, id) => void run(kind, id)} />
  </main>
}

function MonitoringRow({ row, settings, zh, onOpen }: { row: MonitoredSwitch; settings?: MonitoringSettings; zh: boolean; onOpen: () => void }) {
  const over = (value: number | null, threshold: number | null | undefined) => value !== null && threshold !== null && threshold !== undefined && value >= threshold
  const reading = (value: number | null, unit: string, threshold: number | null | undefined) => over(value, threshold)
    ? <span className="inline-flex items-center gap-1 font-medium text-warning"><TriangleAlert aria-hidden="true" className="size-3.5" />{value}{unit}<span className="sr-only">{pick(zh, '（超过阈值）', ' (over the threshold)')}</span></span>
    : value === null ? <span className="text-muted-foreground">—</span> : `${value}${unit}`
  const recent = row.restarted_at && Date.parse(row.restarted_at) > Date.parse(row.polled_at ?? row.restarted_at) - 86_400_000
  let state = <ToneBadge kind="good">{pick(zh, '正常', 'OK')}</ToneBadge>
  if (!row.monitored) state = <ToneBadge kind="muted">{row.snmp_credential_name ? pick(zh, '凭据已停用', 'Credential disabled') : pick(zh, '未监控', 'Not monitored')}</ToneBadge>
  else if (row.error) state = <ToneBadge kind="bad" title={serverText(row.error, zh)}>{pick(zh, '无响应', 'Not responding')}</ToneBadge>
  else if (!row.succeeded_at) state = <ToneBadge kind="muted">{pick(zh, '等待采集', 'Waiting')}</ToneBadge>
  return <TableRow className="cursor-pointer" onClick={onOpen}>
    <TableCell className="whitespace-nowrap">
      <button type="button" className="text-left font-medium underline-offset-4 hover:underline focus-visible:underline focus-visible:outline-none" onClick={(event) => { event.stopPropagation(); onOpen() }}>{row.name}</button>
      <p className="font-mono text-xs text-muted-foreground">{row.management_ip}</p>
    </TableCell>
    <TableCell>{state}</TableCell>
    <TableCell className="text-right text-sm tabular-nums">{reading(row.cpu_percent, '%', settings?.cpu_threshold)}</TableCell>
    <TableCell className="text-right text-sm tabular-nums">{reading(row.memory_percent, '%', settings?.memory_threshold)}</TableCell>
    <TableCell className="text-right text-sm tabular-nums">{reading(row.temperature_c, '°C', settings?.temperature_threshold)}</TableCell>
    <TableCell className="whitespace-nowrap text-sm">{formatUptime(row.uptime_seconds, zh)}
      {recent && row.monitored ? <span className="ml-1.5"><ToneBadge kind="warn">{pick(zh, '刚重启', 'Restarted')}</ToneBadge></span> : null}</TableCell>
    <TableCell className="text-right text-sm tabular-nums">{row.interfaces ? `${row.interfaces_up} / ${row.interfaces}` : '—'}</TableCell>
    <TableCell className="text-right text-sm tabular-nums">{row.response_ms === null ? '—' : `${row.response_ms} ms`}</TableCell>
    <TableCell className="whitespace-nowrap text-sm"><Time value={row.polled_at} zh={zh} /></TableCell>
  </TableRow>
}

const INTERVALS = [1, 2, 5, 10, 15, 30, 60]

/** Interval, thresholds and when they raise alerts; an empty threshold turns its alert off. */
function SettingsDialog({ open, settings, zh, onClose, onSaved, onStale }: {
  open: boolean; settings: MonitoringSettings; zh: boolean; onClose: () => void; onSaved: () => Promise<void>; onStale: () => void
}) {
  const initial = {
    interval: String(settings.interval_minutes), cpu: settings.cpu_threshold?.toString() ?? '', memory: settings.memory_threshold?.toString() ?? '',
    temperature: settings.temperature_threshold?.toString() ?? '', consecutive: String(settings.consecutive_polls), restart: settings.alert_on_restart,
  }
  const [form, setForm] = useState(initial)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const threshold = (value: string, high: number) => {
    const trimmed = value.trim()
    if (!trimmed) return null
    const number = Number(trimmed)
    return Number.isInteger(number) && number >= 1 && number <= high ? number : Number.NaN
  }
  async function save(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const body = {
      version: settings.version, interval_minutes: Number(form.interval), cpu_threshold: threshold(form.cpu, 100), memory_threshold: threshold(form.memory, 100),
      temperature_threshold: threshold(form.temperature, 150), consecutive_polls: Number(form.consecutive), alert_on_restart: form.restart,
    }
    if ([body.cpu_threshold, body.memory_threshold, body.temperature_threshold].some((value) => Number.isNaN(value)) || !Number.isInteger(body.consecutive_polls) || body.consecutive_polls < 1 || body.consecutive_polls > 10) {
      setError(errorText(new Error('invalid monitoring settings'), zh))
      return
    }
    setSaving(true); setError('')
    try { await opsRequest('monitoring/settings', body, 'PATCH'); notifySuccess(pick(zh, '监控设置已保存。', 'Monitoring settings saved.')); await onSaved() }
    catch (cause) { setError(errorText(cause, zh)); if (cause instanceof OpsRequestError && cause.status === 409) onStale() }
    finally { setSaving(false) }
  }
  const number = (id: keyof typeof initial, label: string, unit: string, hint?: string) => <Field>
    <FieldLabel htmlFor={`monitoring-${id}`}>{label}</FieldLabel>
    <div className="flex items-center gap-2">
      <Input id={`monitoring-${id}`} inputMode="numeric" className="w-24 font-mono" value={String(form[id])} placeholder={pick(zh, '关闭', 'Off')}
        onChange={(event) => setForm({ ...form, [id]: event.target.value })} aria-describedby={hint ? `monitoring-${id}-hint` : undefined} />
      <span className="text-sm text-muted-foreground">{unit}</span>
    </div>
    {hint ? <FieldDescription id={`monitoring-${id}-hint`}>{hint}</FieldDescription> : null}
  </Field>
  return <Dialog open={open} onOpenChange={(next) => { if (!next) { setForm(initial); setError(''); onClose() } }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
      <form className="flex flex-col gap-6" noValidate onSubmit={(event) => void save(event)}>
        <DialogHeader>
          <DialogTitle>{pick(zh, '监控设置', 'Monitoring settings')}</DialogTitle>
          <DialogDescription>{pick(zh, '对所有被监控的交换机生效。阈值留空表示不为该项产生告警。', 'These apply to every monitored switch. Leave a threshold empty to raise no alert for it.')}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="monitoring-interval">{pick(zh, '采集间隔', 'Poll every')}</FieldLabel>
            <Select value={form.interval} onValueChange={(interval) => setForm({ ...form, interval })}>
              <SelectTrigger id="monitoring-interval" className="w-40"><SelectValue /></SelectTrigger>
              <SelectContent><SelectGroup>{INTERVALS.map((minutes) => <SelectItem key={minutes} value={String(minutes)}>{pick(zh, `${minutes} 分钟`, `${minutes} minute${minutes === 1 ? '' : 's'}`)}</SelectItem>)}</SelectGroup></SelectContent>
            </Select>
            <FieldDescription>{pick(zh, '间隔越短，图表越细，交换机和数据库的负担也越大。历史数据保留 30 天。', 'Shorter intervals draw finer charts but load the switches and the database more. Samples are kept for 30 days.')}</FieldDescription>
          </Field>
          <div className="grid gap-4 sm:grid-cols-3">
            {number('cpu', pick(zh, 'CPU 阈值', 'CPU threshold'), '%')}
            {number('memory', pick(zh, '内存阈值', 'Memory threshold'), '%')}
            {number('temperature', pick(zh, '温度阈值', 'Temperature threshold'), '°C')}
          </div>
          {number('consecutive', pick(zh, '连续次数', 'Polls in a row'), pick(zh, '次', 'polls'),
            pick(zh, '连续这么多次超过阈值或无响应才产生告警，连续这么多次恢复才自动解决，避免一时的波动反复告警。', 'An alert opens after this many polls in a row over a threshold or without an answer, and resolves after as many back to normal, so a brief spike does not flap.'))}
          <Field orientation="horizontal">
            <Checkbox id="monitoring-restart" checked={form.restart} onCheckedChange={(checked) => setForm({ ...form, restart: checked === true })} />
            <div className="flex flex-col gap-1">
              <FieldLabel htmlFor="monitoring-restart" className="font-normal">{pick(zh, '交换机重启时告警', 'Alert when a switch restarts')}</FieldLabel>
              <FieldDescription>{pick(zh, '发现运行时长归零时产生告警，24 小时后自动解决。', 'Raised when its uptime starts over; resolves itself after 24 hours.')}</FieldDescription>
            </div>
          </Field>
        </FieldGroup>
        {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
        <DialogFooter>
          <DialogClose asChild><Button type="button" variant="outline">{pick(zh, '取消', 'Cancel')}</Button></DialogClose>
          <Button type="submit" disabled={saving}>{saving ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '保存', 'Save')}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}
