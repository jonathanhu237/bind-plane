import { useQuery } from '@tanstack/react-query'
import { Activity } from 'lucide-react'
import { useLayoutEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { serverText } from '@/features/release/messages'
import { cn } from '@/lib/utils'
import { opsRequest, type MetricHistory, type MetricPoint, type MetricRange, type SwitchDetail, type SwitchPort } from './api'
import { Empty, ErrorNote, ToneBadge, Time, formatTime, pick } from './ui'

/** Bits per second in the units network people read: 950 bps, 12.4 Mbps, 1.2 Gbps. */
export function formatBps(bps: number | null | undefined): string {
  if (bps === null || bps === undefined) return '—'
  const units = ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps']
  let value = bps
  let unit = 0
  while (value >= 1000 && unit < units.length - 1) { value /= 1000; unit += 1 }
  return `${unit === 0 || value >= 100 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`
}

/** A port speed as printed on the switch: 100M, 1G, 10G. */
export function formatSpeed(mbps: number | null | undefined): string {
  if (!mbps) return '—'
  return mbps >= 1000 ? `${mbps % 1000 === 0 ? mbps / 1000 : (mbps / 1000).toFixed(1)}G` : `${mbps}M`
}

/** How long a switch has been up, to the two largest units. */
export function formatUptime(seconds: number | null | undefined, zh: boolean): string {
  if (seconds === null || seconds === undefined) return '—'
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  if (days) return zh ? `${days} 天 ${hours} 小时` : `${days}d ${hours}h`
  if (hours) return zh ? `${hours} 小时 ${minutes} 分` : `${hours}h ${minutes}m`
  return zh ? `${Math.max(minutes, 1)} 分钟` : `${Math.max(minutes, 1)}m`
}

/** The busier direction's share of the port speed, in percent. */
export function utilization(port: Pick<SwitchPort, 'in_bps' | 'out_bps' | 'speed_mbps'>): number | null {
  if (!port.speed_mbps || port.in_bps === null || port.out_bps === null) return null
  return Math.min(100, (Math.max(port.in_bps, port.out_bps) / (port.speed_mbps * 1_000_000)) * 100)
}

export type PortState = 'up' | 'down' | 'off'
export const portState = (port: Pick<SwitchPort, 'admin_up' | 'oper_up'>): PortState => (port.admin_up === false ? 'off' : port.oper_up ? 'up' : 'down')

const RANGES: { value: MetricRange; label: [string, string]; seconds: number }[] = [
  { value: '1d', label: ['24 小时', '24 hours'], seconds: 86400 },
  { value: '7d', label: ['7 天', '7 days'], seconds: 7 * 86400 },
  { value: '30d', label: ['30 天', '30 days'], seconds: 30 * 86400 },
]

/** Width of an element as laid out, so the chart draws at its real size and its text stays crisp. */
function useWidth<T extends HTMLElement>() {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    const element = ref.current
    if (!element) return
    // The observer reports the first size as soon as it starts watching.
    const observer = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  return [ref, width] as const
}

type Metric = { key: 'cpu' | 'memory' | 'temperature'; title: string; unit: string; threshold: number | null; current: number | null }

const HEIGHT = 112
const PAD = { top: 8, right: 8, bottom: 22, left: 36 }

/**
 * One measure over time: a 2px line over a 10% wash, a hairline at the alert threshold, and a
 * crosshair shared with the other charts so one readout gives every measure at that time.
 */
function MetricChart({ metric, points, start, end, bucket, hover, onHover, zh }: {
  metric: Metric; points: MetricPoint[]; start: number; end: number; bucket: number; hover: number | null; onHover: (index: number | null) => void; zh: boolean
}) {
  const [ref, width] = useWidth<HTMLDivElement>()
  const values = points.map((point) => point[metric.key])
  const known = values.filter((value): value is number => value !== null)
  const percent = metric.unit === '%'
  const top = percent ? 100 : Math.max(50, Math.ceil((Math.max(...known, metric.threshold ?? 0) + 5) / 10) * 10)
  const plotWidth = Math.max(0, width - PAD.left - PAD.right)
  const plotHeight = HEIGHT - PAD.top - PAD.bottom
  const x = (time: number) => PAD.left + ((time - start) / (end - start)) * plotWidth
  const y = (value: number) => PAD.top + plotHeight - (value / top) * plotHeight
  const times = points.map((point) => new Date(point.t).getTime())
  // A gap of more than two buckets (the switch did not answer) breaks the line.
  const runs: [number, number][][] = []
  points.forEach((_, index) => {
    const value = values[index]
    if (value === null) return
    const run = runs.at(-1)
    const previous = run?.at(-1)
    const joined = run && previous && times[index] - times[index - 1] <= bucket * 2500 && values[index - 1] !== null
    const at: [number, number] = [x(times[index]), y(value)]
    if (joined) run.push(at)
    else runs.push([at])
  })
  const line = runs.map((run) => run.map(([px, py], i) => `${i ? 'L' : 'M'}${px.toFixed(1)},${py.toFixed(1)}`).join('')).join('')
  const area = runs.filter((run) => run.length > 1).map((run) =>
    `M${run[0][0].toFixed(1)},${y(0)}${run.map(([px, py]) => `L${px.toFixed(1)},${py.toFixed(1)}`).join('')}L${run.at(-1)![0].toFixed(1)},${y(0)}Z`).join('')
  const ticks = percent ? [0, 50, 100] : [0, top / 2, top]
  const labelTimes = [0, 1 / 3, 2 / 3, 1].map((share) => start + share * (end - start))
  const timeLabel = (time: number) => {
    const date = new Date(time)
    return end - start > 2 * 86400 * 1000
      ? date.toLocaleDateString(zh ? 'zh-CN' : 'en', { month: '2-digit', day: '2-digit' })
      : date.toLocaleTimeString(zh ? 'zh-CN' : 'en', { hour: '2-digit', minute: '2-digit', hour12: false })
  }
  const nearest = (clientX: number, rect: DOMRect) => {
    if (!times.length) return null
    const time = start + ((clientX - rect.left - PAD.left) / plotWidth) * (end - start)
    let best = 0
    times.forEach((t, i) => { if (Math.abs(t - time) < Math.abs(times[best] - time)) best = i })
    // Over a gap the crosshair shows nothing rather than a sample from hours away.
    return Math.abs(times[best] - time) <= bucket * 3000 ? best : null
  }
  const hovered = hover !== null && values[hover] !== null && values[hover] !== undefined ? hover : null
  const average = known.length ? Math.round(known.reduce((sum, value) => sum + value, 0) / known.length) : null
  // CPU and temperature peaks are the highest single samples; memory barely moves within a bucket.
  const peaks = points.map((p) => (metric.key === 'cpu' ? p.cpu_max : metric.key === 'temperature' ? p.temperature_max : p.memory)).filter((v): v is number => v !== null)
  const peak = peaks.length ? Math.max(...peaks) : null
  const show = (value: number | null) => (value === null ? '—' : `${value}${metric.unit}`)
  return <figure className="flex min-w-0 flex-col gap-1">
    <figcaption className="flex flex-wrap items-baseline justify-between gap-x-3 text-sm">
      <span className="font-medium">{metric.title}</span>
      <span className="text-xs text-muted-foreground">
        {pick(zh, '当前', 'Now')} <span className="font-medium text-foreground">{show(metric.current)}</span>
        {known.length ? <> · {pick(zh, '平均', 'Avg')} {show(average)} · {pick(zh, '最高', 'Peak')} {show(peak)}</> : null}
      </span>
    </figcaption>
    <div ref={ref} tabIndex={known.length ? 0 : -1} role="group" aria-roledescription={pick(zh, '图表', 'chart')} aria-label={pick(zh, `${metric.title}：平均 ${show(average)}，最高 ${show(peak)}。用左右方向键查看各时间点。`, `${metric.title}: average ${show(average)}, peak ${show(peak)}. Use the arrow keys to read each time.`)}
      className="relative rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring"
      onKeyDown={(event) => {
        if (!times.length) return
        if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
          event.preventDefault()
          const from = hover ?? times.length - 1
          onHover(Math.min(times.length - 1, Math.max(0, from + (event.key === 'ArrowLeft' ? -1 : 1))))
        } else if (event.key === 'Escape') onHover(null)
      }}
      onBlur={() => onHover(null)}>
      {width ? <svg width={width} height={HEIGHT} className="block touch-none select-none"
        onPointerMove={(event) => onHover(nearest(event.clientX, event.currentTarget.getBoundingClientRect()))} onPointerLeave={() => onHover(null)}>
        {ticks.map((tick) => <g key={tick}>
          <line x1={PAD.left} x2={width - PAD.right} y1={y(tick)} y2={y(tick)} className="stroke-border" strokeWidth={1} />
          <text x={PAD.left - 6} y={y(tick)} dy="0.32em" textAnchor="end" className="fill-muted-foreground text-[10px] tabular-nums">{tick}{metric.unit}</text>
        </g>)}
        {labelTimes.map((time, i) => <text key={i} x={x(time)} y={HEIGHT - 6} textAnchor={i === 0 ? 'start' : i === labelTimes.length - 1 ? 'end' : 'middle'}
          className="fill-muted-foreground text-[10px] tabular-nums">{timeLabel(time)}</text>)}
        {metric.threshold !== null && metric.threshold <= top ? <g>
          <line x1={PAD.left} x2={width - PAD.right} y1={y(metric.threshold)} y2={y(metric.threshold)} className="stroke-warning" strokeOpacity={0.7} strokeWidth={1} />
          {/* At the left, so it never sits on the newest readings at the right. */}
          <text x={PAD.left + 4} y={y(metric.threshold) - 4} textAnchor="start" className="fill-muted-foreground text-[10px]">{pick(zh, `阈值 ${metric.threshold}${metric.unit}`, `Threshold ${metric.threshold}${metric.unit}`)}</text>
        </g> : null}
        <path d={area} className="fill-chart-1" fillOpacity={0.1} />
        <path d={line} fill="none" className="stroke-chart-1" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
        {/* A lone sample between gaps has no line to sit on, so it shows as a dot. */}
        {runs.filter((run) => run.length === 1).map(([[px, py]], i) => <circle key={i} cx={px} cy={py} r={2} className="fill-chart-1" />)}
        {hovered !== null ? <g pointerEvents="none">
          <line x1={x(times[hovered])} x2={x(times[hovered])} y1={PAD.top} y2={PAD.top + plotHeight} className="stroke-foreground" strokeOpacity={0.35} strokeWidth={1} />
          <circle cx={x(times[hovered])} cy={y(values[hovered]!)} r={4} className="fill-chart-1 stroke-background" strokeWidth={2} />
        </g> : null}
      </svg> : <div style={{ height: HEIGHT }} />}
    </div>
  </figure>
}

/** CPU, memory and temperature over a chosen range, as three charts sharing one time axis. */
function MetricHistoryView({ switchId, current, thresholds, zh }: {
  switchId: string; current: Record<Metric['key'], number | null>; thresholds: Record<Metric['key'], number | null>; zh: boolean
}) {
  const [range, setRange] = useState<MetricRange>('1d')
  const [hover, setHover] = useState<number | null>(null)
  const query = useQuery({
    queryKey: ['ops-metrics', switchId, range], queryFn: () => opsRequest<MetricHistory>(`switches/${switchId}/metrics?range=${range}`),
    refetchInterval: 60_000, placeholderData: (previous) => previous,
  })
  const seconds = RANGES.find((item) => item.value === range)!.seconds
  // The axis ends when the data was read, so a switch that stopped answering shows the gap.
  const end = query.dataUpdatedAt
  const start = end - seconds * 1000
  const points = query.data?.points ?? []
  const bucket = query.data?.bucket_seconds ?? 300
  const metrics: Metric[] = [
    { key: 'cpu', title: 'CPU', unit: '%', threshold: thresholds.cpu, current: current.cpu },
    { key: 'memory', title: pick(zh, '内存', 'Memory'), unit: '%', threshold: thresholds.memory, current: current.memory },
    { key: 'temperature', title: pick(zh, '温度', 'Temperature'), unit: '°C', threshold: thresholds.temperature, current: current.temperature },
  ]
  const point = hover !== null ? points[hover] : null
  const value = (key: Metric['key'], unit: string) => (point?.[key] === null || point?.[key] === undefined ? '—' : `${point[key]}${unit}`)
  return <div className="flex flex-col gap-3">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <div role="group" aria-label={pick(zh, '时间范围', 'Time range')} className="inline-flex rounded-md border p-0.5">
        {RANGES.map((item) => <Button key={item.value} type="button" size="sm" variant={range === item.value ? 'secondary' : 'ghost'} aria-pressed={range === item.value}
          className="h-7 px-2.5" onClick={() => { setRange(item.value); setHover(null) }}>{pick(zh, ...item.label)}</Button>)}
      </div>
      <p className="min-h-5 text-xs text-muted-foreground tabular-nums" aria-live="polite">{point
        ? <>{formatTime(point.t, zh)} · CPU <span className="font-medium text-foreground">{value('cpu', '%')}</span> · {pick(zh, '内存', 'Memory')} <span className="font-medium text-foreground">{value('memory', '%')}</span> · {pick(zh, '温度', 'Temperature')} <span className="font-medium text-foreground">{value('temperature', '°C')}</span></>
        : pick(zh, `每点为 ${Math.round(bucket / 60)} 分钟内的平均值`, `Each point averages ${Math.round(bucket / 60)} minutes`)}</p>
    </div>
    <ErrorNote error={query.error} zh={zh} />
    {!query.data ? null : !points.length
      ? <Empty>{pick(zh, '这段时间还没有采集数据。', 'No samples in this range yet.')}</Empty>
      : <div className={cn('flex flex-col gap-4 transition-opacity', query.isFetching && query.isPlaceholderData && 'opacity-60')}>
        {metrics.map((metric) => <MetricChart key={metric.key} metric={metric} points={points} start={start} end={end} bucket={bucket} hover={hover} onHover={setHover} zh={zh} />)}
      </div>}
  </div>
}

function PortTable({ ports, zh }: { ports: SwitchPort[]; zh: boolean }) {
  const [onlyUp, setOnlyUp] = useState(false)
  const shown = onlyUp ? ports.filter((port) => portState(port) === 'up') : ports
  const label: Record<PortState, string> = { up: pick(zh, '已连接', 'Up'), down: pick(zh, '未连接', 'Down'), off: pick(zh, '已关闭', 'Shut down') }
  return <div className="flex flex-col gap-2">
    <Field orientation="horizontal" className="w-auto">
      <Checkbox id="ports-only-up" checked={onlyUp} onCheckedChange={(checked) => setOnlyUp(checked === true)} />
      <FieldLabel htmlFor="ports-only-up" className="font-normal">{pick(zh, '只看已连接的端口', 'Only ports that are up')}</FieldLabel>
    </Field>
    <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
      <Table>
        <TableHeader><TableRow>
          <TableHead>{pick(zh, '端口', 'Port')}</TableHead><TableHead>{pick(zh, '状态', 'State')}</TableHead><TableHead className="text-right">{pick(zh, '速率', 'Speed')}</TableHead>
          <TableHead className="text-right">{pick(zh, '入', 'In')}</TableHead><TableHead className="text-right">{pick(zh, '出', 'Out')}</TableHead>
          <TableHead className="text-right">{pick(zh, '利用率', 'Use')}</TableHead><TableHead className="text-right">{pick(zh, '新增错包', 'New errors')}</TableHead>
        </TableRow></TableHeader>
        <TableBody>{shown.map((port) => {
          const state = portState(port)
          const use = utilization(port)
          return <TableRow key={port.if_index}>
            <TableCell className="max-w-56"><span className="font-mono text-xs">{port.name}</span>
              {port.alias ? <p className="truncate text-xs text-muted-foreground" title={port.alias}>{port.alias}</p> : null}</TableCell>
            <TableCell><ToneBadge kind={state === 'up' ? 'good' : 'muted'} title={port.oper_changed_at ? pick(zh, `状态变化于 ${formatTime(port.oper_changed_at, zh)}`, `Changed ${formatTime(port.oper_changed_at, zh)}`) : undefined}>{label[state]}</ToneBadge></TableCell>
            <TableCell className="text-right text-xs tabular-nums">{formatSpeed(port.speed_mbps)}</TableCell>
            <TableCell className="whitespace-nowrap text-right text-xs tabular-nums">{state === 'up' ? formatBps(port.in_bps) : '—'}</TableCell>
            <TableCell className="whitespace-nowrap text-right text-xs tabular-nums">{state === 'up' ? formatBps(port.out_bps) : '—'}</TableCell>
            <TableCell className={cn('text-right text-xs tabular-nums', use !== null && use >= 80 && 'font-medium text-warning')}>{state === 'up' && use !== null ? `${use < 1 && use > 0 ? '<1' : Math.round(use)}%` : '—'}</TableCell>
            <TableCell className={cn('text-right text-xs tabular-nums', port.new_errors ? 'font-medium text-destructive' : 'text-muted-foreground')}>{port.new_errors ?? '—'}</TableCell>
          </TableRow>
        })}</TableBody>
      </Table>
    </div>
    <p className="text-xs text-muted-foreground">{pick(zh, '速率是两次采集之间的平均值；新增错包是两次采集之间新出现的输入和输出错包。', 'Rates average the time between two polls; new errors are input and output errors since the previous poll.')}</p>
  </div>
}

/** The monitoring part of a switch's details: its latest poll, its history and its ports. */
export function MonitoringPanel({ detail, zh, onPoll, polling, thresholds }: {
  detail: SwitchDetail; zh: boolean; onPoll: () => void; polling: boolean; thresholds?: Record<Metric['key'], number | null>
}) {
  const m = detail.monitoring
  if (!detail.monitored) {
    return <Empty>{detail.snmp_credential_name
      ? pick(zh, `SNMP 凭据“${detail.snmp_credential_name}”已停用，或交换机已停用，暂不监控。`, `The SNMP credential “${detail.snmp_credential_name}” or the switch is disabled, so it is not monitored.`)
      : pick(zh, '还没有监控。在“编辑”里选择 SNMP 凭据后，系统会定时采集 CPU、内存、温度和端口状态。', 'Not monitored yet. Choose an SNMP credential under Edit and its CPU, memory, temperature and ports are polled on schedule.')}</Empty>
  }
  const ports = detail.interfaces
  const up = ports.filter((port) => portState(port) === 'up').length
  const tile = (label: string, value: string, note?: React.ReactNode) => <div className="min-w-0 rounded-lg border px-3 py-2">
    <dt className="text-xs text-muted-foreground">{label}</dt>
    <dd className="mt-0.5 text-lg font-semibold">{value}</dd>
    {note ? <dd className="text-xs text-muted-foreground">{note}</dd> : null}
  </div>
  return <div className="flex flex-col gap-4">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <p className="text-xs text-muted-foreground">
        {m?.succeeded_at ? <>{pick(zh, '最近采集 ', 'Polled ')}<Time value={m.succeeded_at} zh={zh} />{m.response_ms !== null ? pick(zh, `，响应 ${m.response_ms} ms`, `, answered in ${m.response_ms} ms`) : ''}</> : pick(zh, '等待第一次采集。', 'Waiting for the first poll.')}
        {detail.snmp_credential_name ? pick(zh, ` · SNMP 凭据“${detail.snmp_credential_name}”`, ` · SNMP credential “${detail.snmp_credential_name}”`) : ''}
      </p>
      <Button type="button" size="sm" variant="ghost" disabled={polling} onClick={onPoll}><Activity aria-hidden="true" data-icon="inline-start" />{pick(zh, '立即采集', 'Poll now')}</Button>
    </div>
    {m?.error ? <p className="text-xs text-destructive">{pick(zh, `最近一次采集失败（连续 ${m.failures} 次）：`, `The last poll failed (${m.failures} in a row): `)}{serverText(m.error, zh)}</p> : null}
    {m?.succeeded_at ? <dl className="grid grid-cols-2 gap-2 sm:grid-cols-4">
      {tile(pick(zh, '运行时长', 'Uptime'), formatUptime(m.uptime_seconds, zh), m.restarted_at ? <>{pick(zh, '重启于 ', 'Restarted ')}<Time value={m.restarted_at} zh={zh} /></> : undefined)}
      {tile('CPU', m.cpu_percent === null ? '—' : `${m.cpu_percent}%`, m.cpu_percent === null ? pick(zh, '命令模板没有对应的 OID', 'No OID in its profile') : undefined)}
      {tile(pick(zh, '内存', 'Memory'), m.memory_percent === null ? '—' : `${m.memory_percent}%`)}
      {tile(pick(zh, '端口', 'Ports'), ports.length ? `${up} / ${ports.length}` : '—', pick(zh, '已连接 / 全部', 'up / all'))}
    </dl> : null}
    {m?.sys_descr ? <p className="line-clamp-2 break-words text-xs text-muted-foreground" title={m.sys_descr}>{m.sys_name ? `${m.sys_name} · ` : ''}{m.sys_descr}</p> : null}
    <MetricHistoryView switchId={detail.id} current={{ cpu: m?.cpu_percent ?? null, memory: m?.memory_percent ?? null, temperature: m?.temperature_c ?? null }}
      thresholds={thresholds ?? { cpu: null, memory: null, temperature: null }} zh={zh} />
    {ports.length ? <PortTable ports={ports} zh={zh} /> : null}
  </div>
}
