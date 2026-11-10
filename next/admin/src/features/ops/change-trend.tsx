import { useQuery } from '@tanstack/react-query'
import { FileDiff, X } from 'lucide-react'
import { useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { serverText } from '@/features/release/messages'
import { cn } from '@/lib/utils'
import { opsRequest, type DayChanges, type TrendDay } from './api'
import { DiffDialog } from './device-detail'
import { Empty, ErrorNote, Time, pick } from './ui'

/** "11月11日" or "Nov 11" for a YYYY-MM-DD day, read as written rather than through a time zone. */
export function dayLabel(date: string, zh: boolean): string {
  const [, month, day] = date.split('-').map(Number)
  return zh ? `${month}月${day}日` : `${['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'][month - 1]} ${day}`
}

/** A bar's height as a share of its row; any non-zero count stays visible. */
export function barHeight(value: number, max: number): string {
  if (value <= 0 || max <= 0) return '0'
  return `max(3px, ${Math.round((value / max) * 100)}%)`
}

function dayText(day: TrendDay, zh: boolean) {
  return pick(zh, `${dayLabel(day.date, zh)} · 配置有变化 ${day.changed} 台 · 备份失败 ${day.backup_failed} 台`,
    `${dayLabel(day.date, zh)} · ${day.changed} changed · ${day.backup_failed} failed to back up`)
}

function DayDetail({ date, zh, onClose }: { date: string; zh: boolean; onClose: () => void }) {
  const query = useQuery({ queryKey: ['ops-changes', date], queryFn: () => opsRequest<DayChanges>(`changes?date=${date}`) })
  const [diffing, setDiffing] = useState<{ target: number; base: number } | null>(null)
  const data = query.data
  return <section aria-label={pick(zh, `${dayLabel(date, zh)}的明细`, `${dayLabel(date, zh)} in detail`)} className="flex flex-col gap-4 rounded-xl border p-4">
    <div className="flex items-center justify-between gap-2">
      <h4 className="text-sm font-semibold">{dayLabel(date, zh)}</h4>
      <Button type="button" variant="ghost" size="sm" onClick={onClose}><X aria-hidden="true" data-icon="inline-start" />{pick(zh, '收起', 'Close')}</Button>
    </div>
    <ErrorNote error={query.error} zh={zh} />
    {data ? data.changed.length || data.backup_failed.length ? <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
      <div className="flex min-w-0 flex-col gap-2">
        <h5 className="text-xs font-medium text-muted-foreground">{pick(zh, `配置有变化（${data.changed.length} 台次）`, `Configuration changed (${data.changed.length})`)}</h5>
        {data.changed.length ? <ul className="divide-y rounded-lg border">{data.changed.map((change) => <li key={change.backup_id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
          <span className="min-w-0 flex-1 truncate"><span className="font-medium">{change.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{change.management_ip}</span></span>
          <Time value={change.captured_at} zh={zh} className="text-xs text-muted-foreground tabular-nums" />
          <Button type="button" variant="ghost" size="sm" onClick={() => setDiffing({ target: change.backup_id, base: change.previous_id })}><FileDiff aria-hidden="true" data-icon="inline-start" />{pick(zh, '查看变更', 'Show changes')}</Button>
        </li>)}</ul> : <p className="text-sm text-muted-foreground">{pick(zh, '没有。', 'None.')}</p>}
      </div>
      <div className="flex min-w-0 flex-col gap-2">
        <h5 className="text-xs font-medium text-muted-foreground">{pick(zh, `备份失败（${data.backup_failed.length} 台）`, `Backup failed (${data.backup_failed.length})`)}</h5>
        {data.backup_failed.length ? <ul className="divide-y rounded-lg border">{data.backup_failed.map((failure) => <li key={failure.switch_id} className="flex flex-col gap-0.5 px-3 py-2 text-sm">
          <span className="flex flex-wrap items-baseline justify-between gap-x-3"><span className="min-w-0 truncate"><span className="font-medium">{failure.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{failure.management_ip}</span></span>
            <Time value={failure.finished_at} zh={zh} className="text-xs text-muted-foreground tabular-nums" /></span>
          {failure.message ? <span className="text-xs text-destructive">{serverText(failure.message, zh)}</span> : null}
        </li>)}</ul> : <p className="text-sm text-muted-foreground">{pick(zh, '没有。', 'None.')}</p>}
      </div>
    </div> : <Empty>{pick(zh, '这一天没有配置变化，也没有备份失败。', 'No configuration changed and no backup failed that day.')}</Empty> : null}
    <DiffDialog pair={diffing} zh={zh} onClose={() => setDiffing(null)} />
  </section>
}

/**
 * Configuration changes over the last 30 days, as two rows of bars sharing the days: switches
 * whose configuration changed, and switches whose backup failed. A day opens its switches.
 */
export function ChangeTrend({ trend, zh }: { trend: TrendDay[]; zh: boolean }) {
  const [active, setActive] = useState<number | null>(null)
  const [selected, setSelected] = useState<string | null>(null)
  const buttons = useRef<(HTMLButtonElement | null)[]>([])
  if (!trend.length) return null
  const maxChanged = Math.max(...trend.map((day) => day.changed))
  const maxFailed = Math.max(...trend.map((day) => day.backup_failed))
  const shown = trend[active ?? trend.findIndex((day) => day.date === selected)] ?? trend[trend.length - 1]
  const quiet = maxChanged === 0 && maxFailed === 0
  const move = (index: number) => { const next = Math.max(0, Math.min(trend.length - 1, index)); buttons.current[next]?.focus() }
  const ticks = [0, Math.floor((trend.length - 1) / 2), trend.length - 1]
  return <Card>
    <CardHeader className="flex-row flex-wrap items-start justify-between gap-x-4 gap-y-1">
      <div className="flex min-w-0 flex-col gap-1.5">
        <CardTitle className="text-sm">{pick(zh, '配置变更趋势', 'Configuration changes')}</CardTitle>
        <CardDescription>{pick(zh, '最近 30 天，每天配置有变化和备份失败的交换机数。点某一天查看是哪些交换机。', 'Switches whose configuration changed or whose backup failed, each day for 30 days. Choose a day to see which.')}</CardDescription>
      </div>
      <p aria-live="polite" className="text-xs font-medium tabular-nums">{dayText(shown, zh)}</p>
    </CardHeader>
    <CardContent className="flex flex-col gap-4">
      <div className="flex gap-3">
        <div aria-hidden="true" className="flex w-20 shrink-0 flex-col text-xs text-muted-foreground">
          <div className="flex h-28 flex-col gap-0.5"><span className="font-medium text-foreground">{pick(zh, '配置有变化', 'Changed')}</span><span className="tabular-nums">{pick(zh, `最多 ${maxChanged} 台`, `max ${maxChanged}`)}</span></div>
          <div className="mt-3 flex h-12 flex-col gap-0.5"><span className="font-medium text-foreground">{pick(zh, '备份失败', 'Failed')}</span><span className="tabular-nums">{pick(zh, `最多 ${maxFailed} 台`, `max ${maxFailed}`)}</span></div>
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <div role="group" aria-label={pick(zh, '每天的配置变化', 'Configuration changes per day')} className="grid" style={{ gridTemplateColumns: `repeat(${trend.length}, minmax(0, 1fr))` }} onMouseLeave={() => setActive(null)}>
            {trend.map((day, index) => <button key={day.date} ref={(element) => { buttons.current[index] = element }} type="button"
              aria-label={dayText(day, zh)} aria-pressed={selected === day.date}
              className={cn('flex flex-col rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring', active === index && 'bg-muted/60',
                selected === day.date && 'bg-muted ring-1 ring-inset ring-ring/60')}
              onMouseEnter={() => setActive(index)} onFocus={() => setActive(index)} onBlur={() => setActive(null)}
              onClick={() => setSelected(selected === day.date ? null : day.date)}
              onKeyDown={(event) => {
                if (event.key === 'ArrowLeft') { event.preventDefault(); move(index - 1) }
                if (event.key === 'ArrowRight') { event.preventDefault(); move(index + 1) }
                if (event.key === 'Home') { event.preventDefault(); move(0) }
                if (event.key === 'End') { event.preventDefault(); move(trend.length - 1) }
              }}>
              <span className="flex h-28 items-end border-b border-border"><span className="mx-auto w-3/5 max-w-6 rounded-t-[3px] bg-chart-1" style={{ height: barHeight(day.changed, maxChanged) }} /></span>
              <span className="mt-3 flex h-12 items-end border-b border-border"><span className="mx-auto w-3/5 max-w-6 rounded-t-[3px] bg-destructive" style={{ height: barHeight(day.backup_failed, maxFailed) }} /></span>
            </button>)}
          </div>
          <div aria-hidden="true" className="mt-1.5 flex justify-between text-xs text-muted-foreground tabular-nums">
            {ticks.map((index) => <span key={index}>{dayLabel(trend[index].date, zh)}</span>)}
          </div>
        </div>
      </div>
      {quiet ? <p className="text-sm text-muted-foreground">{pick(zh, '最近 30 天没有配置变化，也没有备份失败。', 'No configuration changed and no backup failed in the last 30 days.')}</p> : null}
      {selected ? <DayDetail key={selected} date={selected} zh={zh} onClose={() => setSelected(null)} /> : null}
    </CardContent>
  </Card>
}
