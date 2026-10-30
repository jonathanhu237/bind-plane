import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { opsRequest, taskState, type Overview } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, Section, TaskStateBadge, Time, isAdmin, kindLabel, pick, useZh } from '@/features/ops/ui'
import { taskScopeText } from '@/features/release/messages'

export const Route = createFileRoute('/_authenticated/ops')({ component: OverviewPage })

function Stat({ label, value, tone }: { label: string; value: number; tone?: 'bad' | 'warn' }) {
  return <div className="flex items-baseline justify-between gap-3 py-1 text-sm">
    <dt className="text-muted-foreground">{label}</dt>
    <dd className={cn('font-medium tabular-nums', value > 0 && tone === 'bad' && 'font-semibold text-destructive', value > 0 && tone === 'warn' && 'font-semibold text-warning')}>{value}</dd>
  </div>
}

function Panel({ title, to, link, children }: { title: string; to: string; link: string; children: React.ReactNode }) {
  return <Card>
    <CardHeader className="flex-row items-center justify-between gap-2 pb-2">
      <CardTitle className="text-sm">{title}</CardTitle>
      <Link to={to} className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{link}</Link>
    </CardHeader>
    <CardContent><dl className="divide-y">{children}</dl></CardContent>
  </Card>
}

function OverviewPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
  const query = useQuery({ queryKey: ['ops-overview'], queryFn: () => opsRequest<Overview>('overview'), enabled: admin, refetchInterval: 15_000 })
  if (!admin) return <AdminOnly zh={zh} />
  const data = query.data
  const alerts = (severity: string) => data?.alerts.find((item) => item.severity === severity)?.count ?? 0
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-8">
    <PageHeader title={pick(zh, '运维概览', 'Operations overview')} description={pick(zh, '交换机连通性、配置备份、合规检查和告警的当前状态。数据每 15 秒刷新。', 'Current reachability, configuration backups, compliance and alerts across your switches. Refreshes every 15 seconds.')} />
    <ErrorNote error={query.error} zh={zh} />
    {data ? <>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
        <Panel title={pick(zh, '设备', 'Devices')} to="/devices" link={pick(zh, '查看设备', 'View devices')}>
          <Stat label={pick(zh, '启用 / 全部', 'Enabled / total')} value={data.switches.enabled} />
          <Stat label={pick(zh, '在线', 'Reachable')} value={data.switches.reachable} />
          <Stat label={pick(zh, '不可达', 'Unreachable')} value={data.switches.unreachable} tone="bad" />
          <Stat label={pick(zh, '尚未检测', 'Not checked yet')} value={data.switches.unknown} />
        </Panel>
        <Panel title={pick(zh, '配置备份', 'Configuration backups')} to="/devices" link={pick(zh, '查看设备', 'View devices')}>
          <Stat label={pick(zh, '已有备份', 'Backed up')} value={data.backups.backed_up} />
          <Stat label={pick(zh, '最近一次失败', 'Last attempt failed')} value={data.backups.failing} tone="bad" />
          <Stat label={pick(zh, '从未备份', 'Never backed up')} value={data.backups.never} tone="warn" />
          <Stat label={pick(zh, '24 小时内新版本', 'New versions, 24 h')} value={data.backups.versions_24h} />
        </Panel>
        <Panel title={pick(zh, '合规检查', 'Compliance')} to="/compliance" link={pick(zh, '查看结果', 'View results')}>
          <Stat label={pick(zh, '未通过的设备', 'Switches failing')} value={data.compliance.failing_switches} tone="bad" />
          <Stat label={pick(zh, '未通过的检查项', 'Failed checks')} value={data.compliance.failed} tone="bad" />
          <Stat label={pick(zh, '通过的检查项', 'Passed checks')} value={data.compliance.passed} />
        </Panel>
        <Panel title={pick(zh, '告警', 'Alerts')} to="/alerts" link={pick(zh, '处理告警', 'Handle alerts')}>
          <Stat label={pick(zh, '严重', 'Critical')} value={alerts('critical')} tone="bad" />
          <Stat label={pick(zh, '警告', 'Warning')} value={alerts('warning')} tone="warn" />
          <Stat label={pick(zh, '提示', 'Info')} value={alerts('info')} />
        </Panel>
      </div>
      <div className="grid grid-cols-1 gap-8 lg:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Section title={pick(zh, '最近任务', 'Recent task runs')} actions={<Link to="/schedules" className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{pick(zh, '全部任务', 'All runs')}</Link>}>
          {data.recent_tasks.length ? <div className="overflow-x-auto rounded-xl border [&_td]:whitespace-nowrap [&_th]:whitespace-nowrap"><Table>
            <TableHeader><TableRow><TableHead>{pick(zh, '开始', 'Started')}</TableHead><TableHead>{pick(zh, '类型', 'Type')}</TableHead><TableHead>{pick(zh, '范围', 'Scope')}</TableHead><TableHead className="text-right">{pick(zh, '成功 / 失败', 'OK / failed')}</TableHead><TableHead>{pick(zh, '状态', 'Status')}</TableHead></TableRow></TableHeader>
            <TableBody>{data.recent_tasks.map((task) => <TableRow key={task.id}>
              <TableCell><Time value={task.created_at} zh={zh} /></TableCell>
              <TableCell>{kindLabel(task.kind, zh)}</TableCell>
              <TableCell className="max-w-48 truncate">{task.schedule_name ?? taskScopeText(task.scope_label, zh)}</TableCell>
              <TableCell className="text-right tabular-nums">{task.succeeded} / {task.failed}</TableCell>
              <TableCell><TaskStateBadge state={taskState(task)} zh={zh} /></TableCell>
            </TableRow>)}</TableBody>
          </Table></div> : <Empty>{pick(zh, '还没有执行过任务。在“计划任务”中创建定时备份，或在“设备”页手动发起。', 'No task has run yet. Create a schedule, or start a backup from the Devices page.')}</Empty>}
        </Section>
        <div className="flex flex-col gap-8">
          <Section title={pick(zh, '最近配置变更', 'Recent configuration changes')}>
            {data.recent_changes.length ? <ul className="divide-y rounded-xl border">{data.recent_changes.map((change) => <li key={change.id} className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm">
              <span className="min-w-0 truncate"><span className="font-medium">{change.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{change.management_ip}</span></span>
              <Time value={change.captured_at} zh={zh} className="shrink-0 text-xs text-muted-foreground" />
            </li>)}</ul> : <Empty>{pick(zh, '还没有配置备份。', 'No configuration backups yet.')}</Empty>}
          </Section>
          <Section title={pick(zh, '即将执行', 'Coming up')}>
            {data.upcoming.length ? <ul className="divide-y rounded-xl border">{data.upcoming.map((item) => <li key={item.id} className="flex items-center justify-between gap-3 px-4 py-2.5 text-sm">
              <span className="min-w-0 truncate">{item.name} <span className="text-xs text-muted-foreground">· {kindLabel(item.kind, zh)}</span></span>
              <Time value={item.next_run_at} zh={zh} className="shrink-0 text-xs text-muted-foreground" />
            </li>)}</ul> : <Empty>{pick(zh, '没有启用的计划任务。', 'No enabled schedules.')}</Empty>}
          </Section>
        </div>
      </div>
    </> : query.isPending ? <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4" aria-busy="true" aria-label={pick(zh, '正在加载', 'Loading')}>
      {[0, 1, 2, 3].map((index) => <Skeleton key={index} className="h-48 rounded-xl" />)}
    </div> : null}
  </main>
}
