import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { TriangleAlert } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Empty, ErrorNote, Section, Time, pick } from '@/features/ops/ui'
import { JobStatusBadge } from './job-parts'
import { canRetry, jobKindLabel, jobListKey, jobsPath, reasonLabel, request, type Job, type JobPage } from './jobs'

type TableProps = {
  jobs: Job[]
  zh: boolean
  admin: boolean
  /** The full history adds the reason column and a retry action. */
  onRetry?: (job: Job) => void
  busy?: boolean
}

/** Jobs newest first; each address opens the job in the release console. */
export function JobTable({ jobs, zh, admin, onRetry, busy = false }: TableProps) {
  return <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
    <Table>
      <TableHeader><TableRow>
        <TableHead>{pick(zh, '时间', 'Time')}</TableHead>
        <TableHead>IP</TableHead>
        <TableHead>{pick(zh, '交换机', 'Switch')}</TableHead>
        <TableHead>{pick(zh, '类型', 'Type')}</TableHead>
        {onRetry ? <TableHead>{pick(zh, '原因', 'Reason')}</TableHead> : null}
        {admin ? <TableHead>{pick(zh, '执行人', 'Actor')}</TableHead> : null}
        <TableHead>{pick(zh, '状态', 'Status')}</TableHead>
        {onRetry ? <TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead> : null}
      </TableRow></TableHeader>
      <TableBody>{jobs.map((job) => <TableRow key={job.id}>
        <TableCell className="whitespace-nowrap text-muted-foreground"><Time value={job.created_at} zh={zh} /></TableCell>
        <TableCell><Button asChild variant="link" className="h-auto p-0 font-mono"><Link to="/release" search={{ job: job.id }}>{job.target_ip}</Link></Button></TableCell>
        <TableCell className="whitespace-nowrap text-muted-foreground" title={job.switch_management_ip}>{job.switch_name ?? '—'}</TableCell>
        <TableCell className="whitespace-nowrap">{jobKindLabel(job.kind, zh)}{job.force ? <span className="text-warning"> · {pick(zh, '强制', 'forced')}</span> : null}</TableCell>
        {onRetry ? <TableCell className="whitespace-nowrap">{reasonLabel(job.reason, zh)}</TableCell> : null}
        {admin ? <TableCell className="max-w-48 truncate" title={job.actor_email}>{job.actor_email ?? '—'}</TableCell> : null}
        <TableCell><JobStatusBadge job={job} zh={zh} /></TableCell>
        {onRetry ? <TableCell className="text-right">{canRetry(job, admin)
          ? <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => onRetry(job)}>{pick(zh, '重新查询', 'Retry query')}</Button> : null}</TableCell> : null}
      </TableRow>)}</TableBody>
    </Table>
  </div>
}

/** The latest jobs: the operator's own, or everyone's for an admin. */
export function RecentJobs({ zh, admin }: { zh: boolean; admin: boolean }) {
  const recent = useQuery({ queryKey: [jobListKey, 'recent'], queryFn: () => request<JobPage>(jobsPath({ pageSize: 8 })), refetchInterval: 15_000 })
  return <Section
    title={admin ? pick(zh, '最近任务', 'Recent jobs') : pick(zh, '我的最近任务', 'My recent jobs')}
    actions={<Link to="/releases" className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{pick(zh, '全部解绑记录', 'All release history')}</Link>}>
    <ErrorNote error={recent.error} zh={zh} />
    {recent.data
      ? recent.data.items.length ? <JobTable jobs={recent.data.items} zh={zh} admin={admin} /> : <Empty>{pick(zh, '还没有解绑任务。在上方输入 IP 地址，开始第一次解绑。', 'No release jobs yet. Enter an IP address above to start your first release.')}</Empty>
      : recent.isPending ? <Skeleton className="h-40 rounded-xl" /> : null}
  </Section>
}

const uncertainReleases = { kind: 'release', status: 'needs_manual_confirmation' }

/** Releases whose outcome is unknown; each one pauses further releases of its address. Nothing when there are none. */
export function UncertainReleases({ zh, admin }: { zh: boolean; admin: boolean }) {
  const uncertain = useQuery({ queryKey: [jobListKey, 'uncertain'], queryFn: () => request<JobPage>(jobsPath({ ...uncertainReleases, pageSize: 5 })), refetchInterval: 15_000 })
  const page = uncertain.data
  if (!page?.total) return null
  const count = page.total
  return <Alert variant="warning">
    <TriangleAlert aria-hidden="true" />
    <AlertTitle>{pick(zh, `${count} 次解绑结果不确定`, `${count} release${count === 1 ? ' has' : 's have'} an uncertain outcome`)}</AlertTitle>
    <AlertDescription className="flex flex-col gap-2">
      <p>{admin
        ? pick(zh, '无法自动判断这些解绑是否生效，对应 IP 的后续解绑已暂停。请登录交换机核实绑定现状。', 'Whether these releases took effect could not be determined, so further releases of these IPs are paused. Check the bindings on the switch.')
        : pick(zh, '无法自动判断这些解绑是否生效，对应 IP 的后续解绑已暂停，需要管理员核实。', 'Whether these releases took effect could not be determined, so further releases of these IPs are paused until an admin checks the switch.')}</p>
      <ul className="flex flex-col gap-1">{page.items.map((job) => <li key={job.id} className="flex flex-wrap items-baseline gap-x-2">
        <Link to="/release" search={{ job: job.id }} className="font-mono font-medium underline underline-offset-4">{job.target_ip}</Link>
        <span>{job.switch_name}</span>
        <Time value={job.created_at} zh={zh} />
      </li>)}</ul>
      {count > page.items.length ? <Link to="/releases" search={uncertainReleases} className="self-start underline underline-offset-4">{pick(zh, `查看全部 ${count} 次`, `View all ${count}`)}</Link> : null}
    </AlertDescription>
  </Alert>
}
