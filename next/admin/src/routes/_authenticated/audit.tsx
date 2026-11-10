import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useInfiniteQuery } from '@tanstack/react-query'
import { Download } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { opsRequest, type AuditEvent, type TaskKind } from '@/features/ops/api'
import { encodeCSV, saveText } from '@/features/ops/csv'
import { AdminOnly, Empty, ErrorNote, PageHeader, actorText, formatTime, isAdmin, kindLabel, pick, useZh } from '@/features/ops/ui'
import { actionName, describeChanges } from '@/features/release/asset-editors'
import { errorText } from '@/features/release/messages'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

export const Route = createFileRoute('/_authenticated/audit')({ component: AuditPage })

const categories = ['credential', 'profile', 'switch', 'network', 'group', 'schedule', 'rule', 'task', 'config', 'alert', 'wecom', 'audit'] as const
// "Every category" travels as this select value and is sent as no filter.
const ALL = 'all'

const two = (n: number) => String(n).padStart(2, '0')
/** A time as spreadsheets sort it, in the browser's time zone. */
const csvTime = (value: string) => {
  const d = new Date(value)
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}:${two(d.getSeconds())}`
}
const fileStamp = (d: Date) => `${d.getFullYear()}${two(d.getMonth() + 1)}${two(d.getDate())}-${two(d.getHours())}${two(d.getMinutes())}`

function AuditPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const { t } = useTranslation()
  const admin = isAdmin(user)
  const [exporting, setExporting] = useState(false)
  const [action, setAction] = useState('')
  const [actor, setActor] = useState('')
  const [q, setQ] = useState('')
  const filters = new URLSearchParams({ action, actor: actor.trim(), q: q.trim() })
  const events = useInfiniteQuery({
    queryKey: ['ops-audit', filters.toString()], enabled: admin, initialPageParam: 0,
    queryFn: ({ pageParam }) => opsRequest<{ events: AuditEvent[]; next_before: number | null }>(`audit?${filters}&before=${pageParam}`),
    getNextPageParam: (last) => last.next_before ?? undefined,
  })
  if (!admin) return <AdminOnly zh={zh} />
  const categoryName: Record<(typeof categories)[number], string> = {
    credential: pick(zh, '凭据', 'Credentials'), profile: pick(zh, '命令模板', 'Profiles'), switch: pick(zh, '交换机', 'Switches'), network: pick(zh, '网段', 'Networks'),
    group: pick(zh, '分组', 'Groups'), schedule: pick(zh, '计划任务', 'Schedules'), rule: pick(zh, '合规规则', 'Compliance rules'), task: pick(zh, '手动任务', 'Manual tasks'),
    config: pick(zh, '读取配置（查看、搜索、下载）', 'Configuration reads'), alert: pick(zh, '告警处理', 'Alert handling'), wecom: pick(zh, '企业微信', 'WeCom'), audit: pick(zh, '导出审计日志', 'Audit exports'),
  }
  const describe = (event: AuditEvent) => event.action === 'task.run'
    ? `${kindLabel(event.changes.kind as TaskKind, zh)} · ${pick(zh, `${String(event.changes.queued)} 台排队`, `${String(event.changes.queued)} queued`)}`
    : describeChanges(event.changes, zh)
  const rows = events.data?.pages.flatMap((page) => page.events) ?? []
  /** Every event the filters match, as the page words them; the export itself is recorded. */
  async function exportCSV() {
    setExporting(true)
    try {
      const result = await opsRequest<{ events: AuditEvent[]; truncated: boolean }>(`audit/export?${filters}`)
      const header = [pick(zh, '时间', 'Time'), pick(zh, '操作者', 'Actor'), pick(zh, '邮箱', 'Email'), pick(zh, '操作', 'Action'), pick(zh, '对象', 'Object'), pick(zh, '变更', 'Changes')]
      const lines = result.events.map((event) => [csvTime(event.created_at), event.actor_name || (event.actor_email ? '' : pick(zh, '系统', 'System')),
        event.actor_email, actionName(event.action, zh), event.object_name, describe(event)])
      saveText(`audit-${fileStamp(new Date())}.csv`, encodeCSV([header, ...lines]))
      const count = result.events.length
      notifySuccess(result.truncated
        ? pick(zh, `已导出最近的 ${count} 条记录。更早的记录请缩小筛选范围后再导出。`, `Exported the latest ${count} events. Narrow the filters to export older ones.`)
        : pick(zh, `已导出 ${count} 条记录。`, `Exported ${count} event${count === 1 ? '' : 's'}.`))
      void events.refetch()
    } catch (cause) { notifyRequestError(cause, t, { description: errorText(cause, zh) }) }
    finally { setExporting(false) }
  }
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '审计日志', 'Audit log')}>
      <Button type="button" variant="outline" disabled={exporting} title={pick(zh, '按当前筛选条件导出全部记录', 'Exports every event the filters match')} onClick={() => void exportCSV()}>
        <Download aria-hidden="true" data-icon="inline-start" />{exporting ? pick(zh, '正在导出…', 'Exporting…') : pick(zh, '导出 CSV', 'Export CSV')}</Button>
    </PageHeader>
    <div className="flex flex-wrap items-center gap-2">
      <Select value={action || ALL} onValueChange={(value) => setAction(value === ALL ? '' : value)}>
        <SelectTrigger aria-label={pick(zh, '操作类别', 'Category')} className="w-auto min-w-36"><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value={ALL}>{pick(zh, '全部类别', 'All categories')}</SelectItem>
          {categories.map((category) => <SelectItem key={category} value={category}>{categoryName[category]}</SelectItem>)}
        </SelectGroup></SelectContent>
      </Select>
      <Input aria-label={pick(zh, '操作者', 'Actor')} placeholder={pick(zh, '操作者姓名或邮箱', 'Actor name or email')} value={actor} onChange={(event) => setActor(event.target.value)} className="w-48" />
      <Input aria-label={pick(zh, '对象', 'Object')} placeholder={pick(zh, '对象名称', 'Object name')} value={q} onChange={(event) => setQ(event.target.value)} className="w-48" />
    </div>
    <ErrorNote error={events.error} zh={zh} />
    {rows.length ? <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap"><Table>
      <TableHeader><TableRow><TableHead>{pick(zh, '时间', 'Time')}</TableHead><TableHead>{pick(zh, '操作者', 'Actor')}</TableHead><TableHead>{pick(zh, '操作', 'Action')}</TableHead><TableHead>{pick(zh, '对象', 'Object')}</TableHead><TableHead>{pick(zh, '变更', 'Changes')}</TableHead></TableRow></TableHeader>
      <TableBody>{rows.map((event) => <TableRow key={event.id}>
        <TableCell className="whitespace-nowrap tabular-nums">{formatTime(event.created_at, zh)}</TableCell>
        <TableCell className="whitespace-nowrap" title={event.actor_email}>{actorText(event.actor_name, event.actor_email, pick(zh, '系统', 'System'))}</TableCell>
        <TableCell className="whitespace-nowrap">{actionName(event.action, zh)}</TableCell>
        <TableCell className="min-w-40 max-w-64 break-words">{event.object_name}</TableCell>
        <TableCell className="min-w-56 max-w-72 break-words text-muted-foreground">{describe(event)}</TableCell>
      </TableRow>)}</TableBody>
    </Table></div> : events.isPending ? null : <Empty>{pick(zh, '没有符合条件的记录。', 'No matching events.')}</Empty>}
    {events.hasNextPage ? <Button type="button" variant="outline" className="self-center" disabled={events.isFetchingNextPage} onClick={() => void events.fetchNextPage()}>{events.isFetchingNextPage ? pick(zh, '正在加载…', 'Loading…') : pick(zh, '加载更多', 'Load more')}</Button> : null}
  </main>
}
