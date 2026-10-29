import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Pencil, Play, Plus } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { describeCron, opsRequest, taskState, type Schedule, type TaskDetail, type TaskKind, type TaskSummary } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, Section, TaskStateBadge, ToneBadge, Time, formatTime, isAdmin, itemStatusLabel, kindLabel, pick, useZh, workerMessage } from '@/features/ops/ui'
import { assetErrorText, callAssets, selectClass, type Inventory } from '@/features/release/asset-editors'

export const Route = createFileRoute('/_authenticated/schedules')({ component: SchedulesPage })

const presets = ['0 */4 * * *', '0 2 * * *', '0 * * * *', '*/10 * * * *']

function ScheduleDialog({ schedule, open, zh, inventory, timezone, onClose, onSaved }: {
  schedule: Schedule | null; open: boolean; zh: boolean; inventory?: Inventory; timezone: string; onClose: () => void; onSaved: () => void
}) {
  const initial = { name: schedule?.name ?? '', kind: schedule?.kind ?? 'config_backup', cron: schedule?.cron ?? '0 */4 * * *', scope: schedule?.scope ?? 'all', group_id: schedule?.group_id ?? '', is_enabled: schedule?.is_enabled ?? true }
  const [form, setForm] = useState(initial)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      const body = { ...form, group_id: form.scope === 'group' ? form.group_id : '' }
      if (schedule) await opsRequest(`schedules/${schedule.id}`, { ...body, version: schedule.version }, 'PATCH')
      else await opsRequest('schedules', body)
      onSaved()
    } catch (cause) { setError(assetErrorText(cause, zh)) }
    finally { setBusy(false) }
  }
  return <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="sm:max-w-lg">
      <form className="space-y-4" onSubmit={(event) => void submit(event)}>
        <DialogHeader>
          <DialogTitle>{schedule ? pick(zh, '编辑计划任务', 'Edit schedule') : pick(zh, '新建计划任务', 'New schedule')}</DialogTitle>
          <DialogDescription>{pick(zh, `按 ${timezone} 时区执行。服务停机期间错过的执行只会补跑一次。`, `Runs in the ${timezone} time zone. Runs missed while the service was down are made up once, not repeatedly.`)}</DialogDescription>
        </DialogHeader>
        <div className="space-y-2"><Label htmlFor="schedule-name">{pick(zh, '名称', 'Name')}</Label><Input id="schedule-name" required value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2"><Label htmlFor="schedule-kind">{pick(zh, '任务类型', 'Task type')}</Label>
            <select id="schedule-kind" className={selectClass} value={form.kind} onChange={(event) => setForm({ ...form, kind: event.target.value as TaskKind })}>
              <option value="config_backup">{kindLabel('config_backup', zh)}</option><option value="reachability">{kindLabel('reachability', zh)}</option>
            </select></div>
          <div className="space-y-2"><Label htmlFor="schedule-scope">{pick(zh, '范围', 'Scope')}</Label>
            <select id="schedule-scope" className={selectClass} value={form.scope} onChange={(event) => setForm({ ...form, scope: event.target.value as 'all' | 'group' })}>
              <option value="all">{pick(zh, '所有启用的交换机', 'All enabled switches')}</option><option value="group">{pick(zh, '指定分组', 'One group')}</option>
            </select></div>
        </div>
        {form.scope === 'group' ? <div className="space-y-2"><Label htmlFor="schedule-group">{pick(zh, '分组', 'Group')}</Label>
          <select id="schedule-group" required className={selectClass} value={form.group_id} onChange={(event) => setForm({ ...form, group_id: event.target.value })}>
            <option value="">—</option>{inventory?.groups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}
          </select></div> : null}
        <div className="space-y-2">
          <Label htmlFor="schedule-cron">{pick(zh, '执行时间（cron：分 时 日 月 周）', 'When (cron: minute hour day month weekday)')}</Label>
          <Input id="schedule-cron" required className="font-mono" value={form.cron} onChange={(event) => setForm({ ...form, cron: event.target.value })} aria-describedby="schedule-cron-reading" />
          <p id="schedule-cron-reading" className="text-xs text-muted-foreground">{describeCron(form.cron, zh)}</p>
          <div className="flex flex-wrap gap-1.5">{presets.map((preset) => <Button key={preset} type="button" variant="outline" size="sm" className="h-7 text-xs" onClick={() => setForm({ ...form, cron: preset })}>{describeCron(preset, zh)}</Button>)}</div>
        </div>
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.is_enabled} onChange={(event) => setForm({ ...form, is_enabled: event.target.checked })} />{pick(zh, '启用', 'Enabled')}</label>
        {error ? <p role="alert" className="rounded-md border border-destructive/40 px-3 py-2 text-sm text-destructive">{error}</p> : null}
        <DialogFooter><Button type="button" variant="outline" onClick={onClose}>{pick(zh, '取消', 'Cancel')}</Button><Button type="submit" disabled={busy}>{busy ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '保存', 'Save')}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}

function TaskDialog({ taskId, zh, onClose }: { taskId: string | null; zh: boolean; onClose: () => void }) {
  const query = useQuery({
    queryKey: ['ops-task', taskId], queryFn: () => opsRequest<TaskDetail>(`tasks/${taskId}`), enabled: taskId !== null,
    refetchInterval: (q) => (q.state.data && taskState(q.state.data) === 'running' ? 3000 : false),
  })
  const task = query.data
  const tone = (status: string) => status === 'succeeded' ? 'good' : status === 'failed' ? 'bad' : 'muted'
  return <Dialog open={taskId !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="flex max-h-[90dvh] flex-col sm:max-w-3xl">
      <DialogHeader>
        <DialogTitle>{task ? `${kindLabel(task.kind, zh)} · ${task.schedule_name ?? task.scope_label}` : pick(zh, '任务详情', 'Task run')}</DialogTitle>
        <DialogDescription>{task ? pick(zh, `${formatTime(task.created_at, zh)} 由 ${task.actor_email ?? '计划任务'} 发起 · 成功 ${task.succeeded} · 失败 ${task.failed} · 跳过 ${task.skipped} · 配置变化 ${task.changed}`, `Started ${formatTime(task.created_at, zh)} by ${task.actor_email ?? 'schedule'} · ${task.succeeded} succeeded · ${task.failed} failed · ${task.skipped} skipped · ${task.changed} changed`) : ' '}</DialogDescription>
      </DialogHeader>
      <ErrorNote error={query.error} zh={zh} />
      {task ? <div className="min-h-0 overflow-auto rounded-md border"><Table>
        <TableHeader><TableRow><TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '状态', 'Status')}</TableHead><TableHead>{pick(zh, '说明', 'Detail')}</TableHead><TableHead>{pick(zh, '完成', 'Finished')}</TableHead></TableRow></TableHeader>
        <TableBody>{task.items.map((item) => <TableRow key={item.id}>
          <TableCell><span className="font-medium">{item.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{item.management_ip}</span></TableCell>
          <TableCell><ToneBadge kind={tone(item.status)}>{itemStatusLabel(item.status, zh)}</ToneBadge></TableCell>
          <TableCell className="max-w-72 break-words text-xs text-muted-foreground">{workerMessage(item.message, zh)}{item.changed ? <span className="ml-1 text-emerald-700 dark:text-emerald-300">●</span> : null}</TableCell>
          <TableCell><Time value={item.finished_at} zh={zh} className="text-xs" /></TableCell>
        </TableRow>)}</TableBody>
      </Table></div> : null}
    </DialogContent>
  </Dialog>
}

function SchedulesPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
  const schedules = useQuery({ queryKey: ['ops-schedules'], queryFn: () => opsRequest<{ schedules: Schedule[]; timezone: string }>('schedules'), enabled: admin })
  const tasks = useQuery({
    queryKey: ['ops-tasks'], queryFn: () => opsRequest<TaskSummary[]>('tasks'), enabled: admin,
    refetchInterval: (q) => (q.state.data?.some((task) => taskState(task) === 'running') ? 3000 : 15_000),
  })
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin, staleTime: 60_000 })
  const [editing, setEditing] = useState<Schedule | null | 'new'>(null)
  const [taskId, setTaskId] = useState<string | null>(null)
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  async function act(action: () => Promise<unknown>, done: string) {
    setBusy(true); setMessage('')
    try { await action(); setMessage(done); await Promise.all([schedules.refetch(), tasks.refetch()]) }
    catch (cause) { setMessage(assetErrorText(cause, zh)) }
    finally { setBusy(false) }
  }
  if (!admin) return <AdminOnly zh={zh} />
  const timezone = schedules.data?.timezone ?? 'Asia/Shanghai'
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-8">
    <PageHeader title={pick(zh, '计划任务', 'Schedules')} description={pick(zh, '定时备份配置或检测连通性，并查看每次执行的逐台结果。只有配置发生变化时才会保存新版本。', 'Back up configurations or check reachability on a schedule, and review each run switch by switch. A new configuration version is stored only when something changed.')}>
      <Button type="button" onClick={() => setEditing('new')}><Plus aria-hidden="true" />{pick(zh, '新建计划', 'New schedule')}</Button>
    </PageHeader>
    {message ? <p role="status" className="rounded-md border px-3 py-2 text-sm">{message}</p> : null}
    <ErrorNote error={schedules.error ?? tasks.error} zh={zh} />
    <Section title={pick(zh, '计划', 'Schedules')}>
      {schedules.data?.schedules.length ? <div className="overflow-x-auto rounded-xl border"><Table>
        <TableHeader><TableRow><TableHead>{pick(zh, '名称', 'Name')}</TableHead><TableHead>{pick(zh, '类型', 'Type')}</TableHead><TableHead>{pick(zh, '执行时间', 'When')}</TableHead><TableHead>{pick(zh, '范围', 'Scope')}</TableHead><TableHead>{pick(zh, '下次执行', 'Next run')}</TableHead><TableHead>{pick(zh, '上次执行', 'Last run')}</TableHead><TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead></TableRow></TableHeader>
        <TableBody>{schedules.data.schedules.map((schedule) => <TableRow key={schedule.id}>
          <TableCell className="font-medium">{schedule.name}{schedule.is_enabled ? null : <ToneBadge kind="muted">{pick(zh, '已停用', 'Disabled')}</ToneBadge>}</TableCell>
          <TableCell>{kindLabel(schedule.kind, zh)}</TableCell>
          <TableCell><span title={schedule.cron}>{describeCron(schedule.cron, zh)}</span></TableCell>
          <TableCell>{schedule.scope === 'all' ? pick(zh, '全部', 'All') : schedule.group_name}</TableCell>
          <TableCell>{schedule.is_enabled ? <Time value={schedule.next_run_at} zh={zh} /> : '—'}</TableCell>
          <TableCell><Time value={schedule.last_run_at} zh={zh} /></TableCell>
          <TableCell className="text-right"><div className="flex justify-end gap-1">
            <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void act(() => opsRequest(`schedules/${schedule.id}/run`, {}), pick(zh, `“${schedule.name}”已开始执行。`, `“${schedule.name}” started.`))}><Play aria-hidden="true" />{pick(zh, '立即执行', 'Run now')}</Button>
            <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void act(() => opsRequest(`schedules/${schedule.id}`, { version: schedule.version, name: schedule.name, is_enabled: !schedule.is_enabled }, 'PATCH'), schedule.is_enabled ? pick(zh, '已停用。', 'Disabled.') : pick(zh, '已启用。', 'Enabled.'))}>{schedule.is_enabled ? pick(zh, '停用', 'Disable') : pick(zh, '启用', 'Enable')}</Button>
            <Button type="button" variant="ghost" size="sm" disabled={busy} aria-label={`${pick(zh, '编辑', 'Edit')} ${schedule.name}`} onClick={() => setEditing(schedule)}><Pencil aria-hidden="true" /></Button>
          </div></TableCell>
        </TableRow>)}</TableBody>
      </Table></div> : <Empty>{pick(zh, '还没有计划任务。建议先创建“每 4 小时备份全部设备”。', 'No schedules yet. A good first one: back up every switch every 4 hours.')}</Empty>}
    </Section>
    <Section title={pick(zh, '执行记录（保留 30 天）', 'Runs (kept for 30 days)')}>
      {tasks.data?.length ? <div className="overflow-x-auto rounded-xl border"><Table>
        <TableHeader><TableRow><TableHead>{pick(zh, '开始', 'Started')}</TableHead><TableHead>{pick(zh, '类型', 'Type')}</TableHead><TableHead>{pick(zh, '来源', 'Source')}</TableHead><TableHead className="text-right">{pick(zh, '成功 / 失败 / 跳过', 'OK / failed / skipped')}</TableHead><TableHead className="text-right">{pick(zh, '配置变化', 'Changed')}</TableHead><TableHead>{pick(zh, '状态', 'Status')}</TableHead></TableRow></TableHeader>
        <TableBody>{tasks.data.map((task) => <TableRow key={task.id} className="cursor-pointer" onClick={() => setTaskId(task.id)}>
          <TableCell><Time value={task.created_at} zh={zh} /></TableCell>
          <TableCell>{kindLabel(task.kind, zh)}</TableCell>
          <TableCell className="max-w-64 truncate">{task.schedule_name ? `${task.schedule_name}` : `${task.scope_label} · ${task.actor_email ?? ''}`}</TableCell>
          <TableCell className="text-right tabular-nums">{task.succeeded} / {task.failed} / {task.skipped}{taskState(task) === 'running' ? <span className="ml-1 text-muted-foreground">({pick(zh, `剩余 ${task.queued + task.running}`, `${task.queued + task.running} left`)})</span> : null}</TableCell>
          <TableCell className="text-right tabular-nums">{task.kind === 'config_backup' ? task.changed : '—'}</TableCell>
          <TableCell><TaskStateBadge state={taskState(task)} zh={zh} /></TableCell>
        </TableRow>)}</TableBody>
      </Table></div> : <Empty>{pick(zh, '还没有执行记录。', 'No runs yet.')}</Empty>}
    </Section>
    {editing !== null ? <ScheduleDialog key={editing === 'new' ? 'new' : `${editing.id}:${editing.version}`} schedule={editing === 'new' ? null : editing} open zh={zh} inventory={inventory.data} timezone={timezone}
      onClose={() => setEditing(null)} onSaved={() => { setEditing(null); setMessage(pick(zh, '已保存。', 'Saved.')); void schedules.refetch() }} /> : null}
    <TaskDialog taskId={taskId} zh={zh} onClose={() => setTaskId(null)} />
  </main>
}
