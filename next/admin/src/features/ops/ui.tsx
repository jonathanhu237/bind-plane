import { ChevronLeft, ChevronRight, CircleAlert, Lock } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Empty as EmptyState, EmptyDescription, EmptyHeader } from '@/components/ui/empty'
import { cn } from '@/lib/utils'
import { errorText, serverText } from '@/features/release/messages'
import type { User } from '@/shared/api/contracts'
import type { Severity, TaskKind, TaskState } from './api'

export function useZh(): boolean {
  const { i18n } = useTranslation()
  return i18n.language.startsWith('zh')
}

export const pick = (zh: boolean, chinese: string, english: string) => (zh ? chinese : english)

export const isAdmin = (user: User) => Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin'))

export function formatTime(value: string | null | undefined, zh: boolean): string {
  return value ? new Date(value).toLocaleString(zh ? 'zh-CN' : 'en', { hour12: false }) : '—'
}

function relative(value: string, zh: boolean): string {
  const seconds = Math.round((new Date(value).getTime() - Date.now()) / 1000)
  const format = new Intl.RelativeTimeFormat(zh ? 'zh-CN' : 'en', { numeric: 'auto' })
  for (const [unit, size] of [['day', 86400], ['hour', 3600], ['minute', 60]] as const) {
    if (Math.abs(seconds) >= size) return format.format(Math.round(seconds / size), unit)
  }
  return format.format(seconds, 'second')
}

/** Relative time with the exact timestamp one hover away. */
export function Time({ value, zh, className }: { value: string | null | undefined; zh: boolean; className?: string }) {
  if (!value) return <span className={cn('text-muted-foreground', className)}>—</span>
  return <time dateTime={value} title={formatTime(value, zh)} className={cn('tabular-nums', className)}>{relative(value, zh)}</time>
}

export function PageHeader({ title, description, children }: { title: string; description: React.ReactNode; children?: React.ReactNode }) {
  return <header className="flex flex-wrap items-end justify-between gap-4">
    <div className="flex flex-col gap-1.5">
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      <p className="max-w-[70ch] text-sm text-muted-foreground">{description}</p>
    </div>
    {children ? <div className="flex flex-wrap items-center gap-2">{children}</div> : null}
  </header>
}

export function AdminOnly({ zh }: { zh: boolean }) {
  return <Alert><Lock aria-hidden="true" /><AlertDescription>{pick(zh, '仅管理员可以使用设备运维功能。', 'Device operations are available to admins only.')}</AlertDescription></Alert>
}

/** A failed request in the interface language; renders nothing without an error. */
export function ErrorNote({ error, zh }: { error: unknown; zh: boolean }) {
  if (!error) return null
  return <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{errorText(error, zh)}</AlertDescription></Alert>
}

const tone = {
  good: 'border-success/40 text-success',
  bad: 'border-destructive/50 text-destructive',
  warn: 'border-warning/50 text-warning',
  muted: 'text-muted-foreground',
}

export function ToneBadge({ kind, children, title }: { kind: keyof typeof tone; children: React.ReactNode; title?: string }) {
  return <Badge variant="outline" title={title} className={cn('whitespace-nowrap font-normal', tone[kind])}>{children}</Badge>
}

export function ReachabilityBadge({ reachable, latency, error, zh }: { reachable: boolean | null; latency?: number | null; error?: string | null; zh: boolean }) {
  if (reachable === null) return <ToneBadge kind="muted">{pick(zh, '未检测', 'Not checked')}</ToneBadge>
  return reachable
    ? <ToneBadge kind="good">{pick(zh, '在线', 'Reachable')}{latency != null ? ` · ${latency} ms` : ''}</ToneBadge>
    : <ToneBadge kind="bad" title={error ? serverText(error, zh) : undefined}>{pick(zh, '不可达', 'Unreachable')}</ToneBadge>
}

export function SeverityBadge({ severity, zh }: { severity: Severity; zh: boolean }) {
  const label = { critical: pick(zh, '严重', 'Critical'), warning: pick(zh, '警告', 'Warning'), info: pick(zh, '提示', 'Info') }[severity]
  return <ToneBadge kind={severity === 'critical' ? 'bad' : severity === 'warning' ? 'warn' : 'muted'}>{label}</ToneBadge>
}

export function TaskStateBadge({ state, zh }: { state: TaskState; zh: boolean }) {
  const labels: Record<TaskState, string> = {
    running: pick(zh, '进行中', 'Running'), succeeded: pick(zh, '成功', 'Succeeded'), partial: pick(zh, '部分失败', 'Partly failed'),
    failed: pick(zh, '失败', 'Failed'), empty: pick(zh, '无设备', 'Nothing to do'),
  }
  const kind = state === 'succeeded' ? 'good' : state === 'failed' ? 'bad' : state === 'partial' ? 'warn' : 'muted'
  return <ToneBadge kind={kind}>{labels[state]}</ToneBadge>
}

export const itemStatusLabel = (status: string, zh: boolean) =>
  ({ queued: pick(zh, '排队中', 'Queued'), running: pick(zh, '执行中', 'Running'), succeeded: pick(zh, '成功', 'Succeeded'), failed: pick(zh, '失败', 'Failed'), skipped: pick(zh, '跳过', 'Skipped') })[status] ?? status

/** A worker message in the interface language; unknown detail is shown as sent. */
export const workerMessage = (message: string | null, zh: boolean) => serverText(message, zh)

export const kindLabel = (kind: TaskKind, zh: boolean) => (kind === 'config_backup' ? pick(zh, '配置备份', 'Config backup') : pick(zh, '连通性检测', 'Reachability check'))

export function Pager({ page, total, pageSize, onPage, zh }: { page: number; total: number; pageSize: number; onPage: (page: number) => void; zh: boolean }) {
  const pages = Math.max(1, Math.ceil(total / pageSize))
  if (total <= pageSize) return <p className="text-xs text-muted-foreground tabular-nums">{pick(zh, `共 ${total} 条`, `${total} total`)}</p>
  return <nav aria-label={pick(zh, '分页', 'Pagination')} className="flex items-center gap-2 text-sm">
    <Button type="button" variant="outline" size="sm" disabled={page <= 1} onClick={() => onPage(page - 1)} aria-label={pick(zh, '上一页', 'Previous page')}><ChevronLeft aria-hidden="true" /></Button>
    <span className="tabular-nums text-muted-foreground">{pick(zh, `第 ${page} / ${pages} 页 · 共 ${total} 条`, `Page ${page} of ${pages} · ${total} total`)}</span>
    <Button type="button" variant="outline" size="sm" disabled={page >= pages} onClick={() => onPage(page + 1)} aria-label={pick(zh, '下一页', 'Next page')}><ChevronRight aria-hidden="true" /></Button>
  </nav>
}

/** A titled section of a page; one level of container, never nested. */
export function Section({ title, actions, children, className }: { title: string; actions?: React.ReactNode; children: React.ReactNode; className?: string }) {
  return <section className={cn('flex min-w-0 flex-col gap-3', className)}>
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h2 className="text-base font-semibold">{title}</h2>
      {actions ? <div className="flex flex-wrap items-center gap-2">{actions}</div> : null}
    </div>
    {children}
  </section>
}

export function Empty({ children }: { children: React.ReactNode }) {
  return <EmptyState className="border md:p-6"><EmptyHeader><EmptyDescription>{children}</EmptyDescription></EmptyHeader></EmptyState>
}
