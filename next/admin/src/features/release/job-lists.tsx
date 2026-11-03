import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate } from '@tanstack/react-router'
import { TriangleAlert } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Empty, ErrorNote, Section, Time, actorText, pick } from '@/features/ops/ui'
import { JobStatusBadge } from './job-parts'
import { canRetry, jobListKey, jobsPath, reasonLabel, releaseErrorText, request, type Job, type JobPage } from './jobs'

type TableProps = {
  jobs: Job[]
  zh: boolean
  admin: boolean
  /** The full history adds the reason column and a retry action. */
  onRetry?: (job: Job) => void
  busy?: boolean
}

/** Releases newest first; each address opens the release in the console. */
export function JobTable({ jobs, zh, admin, onRetry, busy = false }: TableProps) {
  return <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
    <Table>
      <TableHeader><TableRow>
        <TableHead>{pick(zh, '时间', 'Time')}</TableHead>
        <TableHead>IP</TableHead>
        <TableHead>{pick(zh, '交换机', 'Switch')}</TableHead>
        {onRetry ? <TableHead>{pick(zh, '原因', 'Reason')}</TableHead> : null}
        {admin ? <TableHead>{pick(zh, '执行人', 'Actor')}</TableHead> : null}
        <TableHead>{pick(zh, '状态', 'Status')}</TableHead>
        {onRetry ? <TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead> : null}
      </TableRow></TableHeader>
      <TableBody>{jobs.map((job) => <TableRow key={job.id}>
        <TableCell className="whitespace-nowrap text-muted-foreground"><Time value={job.created_at} zh={zh} /></TableCell>
        <TableCell><Button asChild variant="link" className="h-auto p-0 font-mono"><Link to="/release" search={{ job: job.id }}>{job.target_ip}</Link></Button></TableCell>
        <TableCell className="whitespace-nowrap text-muted-foreground" title={job.switch_management_ip}>{job.switch_name ?? '—'}</TableCell>
        {onRetry ? <TableCell className="whitespace-nowrap">{reasonLabel(job.reason, zh)}</TableCell> : null}
        {admin ? <TableCell className="max-w-48 truncate" title={job.actor_email}>{actorText(job.actor_name, job.actor_email)}</TableCell> : null}
        <TableCell className="whitespace-nowrap"><JobStatusBadge job={job} zh={zh} />{job.force ? <span className="ml-2 text-xs text-warning">{pick(zh, '强制', 'Forced')}</span> : null}</TableCell>
        {onRetry ? <TableCell className="text-right">{canRetry(job)
          ? <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => onRetry(job)}>{pick(zh, '重新解绑', 'Release again')}</Button> : null}</TableCell> : null}
      </TableRow>)}</TableBody>
    </Table>
  </div>
}

/** The latest releases: the operator's own, or everyone's for an admin. */
export function RecentJobs({ zh, admin }: { zh: boolean; admin: boolean }) {
  const recent = useQuery({ queryKey: [jobListKey, 'recent'], queryFn: () => request<JobPage>(jobsPath({ kind: 'release', pageSize: 8 })), refetchInterval: 15_000 })
  return <Section
    title={admin ? pick(zh, '最近解绑', 'Recent releases') : pick(zh, '我的最近解绑', 'My recent releases')}
    actions={<Link to="/releases" className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{pick(zh, '全部解绑记录', 'All release history')}</Link>}>
    <ErrorNote error={recent.error} zh={zh} />
    {recent.data
      ? recent.data.items.length ? <JobTable jobs={recent.data.items} zh={zh} admin={admin} /> : <Empty>{pick(zh, '还没有解绑记录。需要解绑时，点“发起解绑”。', 'No releases yet. Choose New release to start one.')}</Empty>
      : recent.isPending ? <Skeleton className="h-40 rounded-xl" /> : null}
  </Section>
}

const unknownOutcomes = { kind: 'release', status: 'needs_manual_confirmation' }

/**
 * Releases whose outcome is unknown and that no later release of the address has followed, each with
 * a way to release it again. Nothing when there are none.
 */
export function UncertainReleases({ zh }: { zh: boolean }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const uncertain = useQuery({ queryKey: [jobListKey, 'unknown'], queryFn: () => request<JobPage>(jobsPath({ ...unknownOutcomes, latest: true, pageSize: 5 })), refetchInterval: 15_000 })
  const page = uncertain.data
  if (!page?.total) return null
  async function releaseAgain(job: Job) {
    setBusy(true); setError('')
    try {
      const next = await request<Job>(`/api/releases/jobs/${job.id}/retry`, {})
      void queryClient.invalidateQueries({ queryKey: [jobListKey] })
      void navigate({ to: '/release', search: { job: next.job_id || next.id } })
    } catch (cause) { setError(releaseErrorText(cause, zh)) }
    finally { setBusy(false) }
  }
  const count = page.total
  return <Alert variant="warning">
    <TriangleAlert aria-hidden="true" />
    <AlertTitle>{pick(zh, `${count} 次解绑结果未知`, `${count} release${count === 1 ? '' : 's'} with an unknown outcome`)}</AlertTitle>
    <AlertDescription className="flex flex-col gap-2">
      <p>{pick(zh, '解绑命令可能已经发出，但无法确定静态绑定是否还在。点“重新解绑”会先查询交换机：已删除就不再发送命令，还在就删除。', 'A release command may have run, but whether the static binding is gone is unknown. Release again queries the switch first: nothing is sent if the binding is gone, and it is deleted if not.')}</p>
      <ul className="flex flex-col gap-2">{page.items.map((job) => <li key={job.id} className="flex flex-wrap items-center gap-x-2 gap-y-1">
        <Link to="/release" search={{ job: job.id }} className="font-mono font-medium underline underline-offset-4">{job.target_ip}</Link>
        <span>{job.switch_name}</span>
        <Time value={job.created_at} zh={zh} />
        {canRetry(job) ? <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void releaseAgain(job)}>{pick(zh, '重新解绑', 'Release again')}</Button> : null}
      </li>)}</ul>
      {error ? <p role="alert">{error}</p> : null}
      {count > page.items.length ? <Link to="/releases" search={unknownOutcomes} className="self-start underline underline-offset-4">{pick(zh, '查看所有结果未知的解绑', 'View every release with an unknown outcome')}</Link> : null}
    </AlertDescription>
  </Alert>
}
