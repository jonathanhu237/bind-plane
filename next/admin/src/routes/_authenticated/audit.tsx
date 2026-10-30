import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useInfiniteQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { opsRequest, type AuditEvent, type TaskKind } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, formatTime, isAdmin, kindLabel, pick, useZh } from '@/features/ops/ui'
import { actionName, describeChanges } from '@/features/release/asset-editors'

export const Route = createFileRoute('/_authenticated/audit')({ component: AuditPage })

const categories = ['credential', 'profile', 'switch', 'network', 'group', 'schedule', 'rule', 'task', 'config', 'alert'] as const
// "Every category" travels as this select value and is sent as no filter.
const ALL = 'all'

function AuditPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
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
    config: pick(zh, '查看与对比配置', 'Configuration reads'), alert: pick(zh, '告警处理', 'Alert handling'),
  }
  const describe = (event: AuditEvent) => event.action === 'task.run'
    ? `${kindLabel(event.changes.kind as TaskKind, zh)} · ${pick(zh, `${String(event.changes.queued)} 台排队`, `${String(event.changes.queued)} queued`)}`
    : describeChanges(event.changes, zh)
  const rows = events.data?.pages.flatMap((page) => page.events) ?? []
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '审计日志', 'Audit log')} description={pick(zh, '所有资产变更、导入、计划和规则调整、手动任务、配置查看与告警确认。永久保留，不会自动清理；密码类字段只记录“已更换”。', 'Every asset change, import, schedule and rule edit, manual task, configuration read and alert acknowledgement. Kept indefinitely; secrets are only ever recorded as replaced.')} />
    <div className="flex flex-wrap items-center gap-2">
      <Select value={action || ALL} onValueChange={(value) => setAction(value === ALL ? '' : value)}>
        <SelectTrigger aria-label={pick(zh, '操作类别', 'Category')} className="w-auto min-w-36"><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value={ALL}>{pick(zh, '全部类别', 'All categories')}</SelectItem>
          {categories.map((category) => <SelectItem key={category} value={category}>{categoryName[category]}</SelectItem>)}
        </SelectGroup></SelectContent>
      </Select>
      <Input aria-label={pick(zh, '操作者', 'Actor')} placeholder={pick(zh, '操作者邮箱', 'Actor email')} value={actor} onChange={(event) => setActor(event.target.value)} className="w-48" />
      <Input aria-label={pick(zh, '对象', 'Object')} placeholder={pick(zh, '对象名称', 'Object name')} value={q} onChange={(event) => setQ(event.target.value)} className="w-48" />
    </div>
    <ErrorNote error={events.error} zh={zh} />
    {rows.length ? <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap"><Table>
      <TableHeader><TableRow><TableHead>{pick(zh, '时间', 'Time')}</TableHead><TableHead>{pick(zh, '操作者', 'Actor')}</TableHead><TableHead>{pick(zh, '操作', 'Action')}</TableHead><TableHead>{pick(zh, '对象', 'Object')}</TableHead><TableHead>{pick(zh, '变更', 'Changes')}</TableHead></TableRow></TableHeader>
      <TableBody>{rows.map((event) => <TableRow key={event.id}>
        <TableCell className="whitespace-nowrap tabular-nums">{formatTime(event.created_at, zh)}</TableCell>
        <TableCell className="whitespace-nowrap">{event.actor_email}</TableCell>
        <TableCell className="whitespace-nowrap">{actionName(event.action, zh)}</TableCell>
        <TableCell className="min-w-40 max-w-64 break-words">{event.object_name}</TableCell>
        <TableCell className="min-w-56 max-w-72 break-words text-muted-foreground">{describe(event)}</TableCell>
      </TableRow>)}</TableBody>
    </Table></div> : events.isPending ? null : <Empty>{pick(zh, '没有符合条件的记录。', 'No matching events.')}</Empty>}
    {events.hasNextPage ? <Button type="button" variant="outline" className="self-center" disabled={events.isFetchingNextPage} onClick={() => void events.fetchNextPage()}>{events.isFetchingNextPage ? pick(zh, '正在加载…', 'Loading…') : pick(zh, '加载更多', 'Load more')}</Button> : null}
  </main>
}
