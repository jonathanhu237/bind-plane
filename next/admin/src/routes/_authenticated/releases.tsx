import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, Lock, Plus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Empty, ErrorNote, PageHeader, Pager, isAdmin, pick, useZh } from '@/features/ops/ui'
import { JobTable } from '@/features/release/job-lists'
import { canRelease, jobKindLabel, jobListKey, jobsPath, releaseErrorText, request, statusLabel, type Job, type JobPage } from '@/features/release/jobs'

type HistorySearch = { q?: string; kind?: string; status?: string; page?: number }
const filterKeys = ['q', 'kind', 'status'] as const
// A filter set to everything travels as this select value and leaves the search parameter out.
const ANY = 'any'
const kinds = ['release', 'pre_release_query']
// A query waiting for confirmation shows as confirmed once released, so that status is not offered as a filter.
const statuses = ['succeeded', 'failed', 'timeout', 'needs_manual_confirmation', 'cancelled', 'running']

export const Route = createFileRoute('/_authenticated/releases')({
  validateSearch: (search: Record<string, unknown>): HistorySearch => {
    const result: HistorySearch = {}
    for (const key of filterKeys) {
      if (typeof search[key] === 'string' && search[key]) result[key] = search[key]
    }
    const page = Number(search.page)
    if (Number.isInteger(page) && page > 1) result.page = page
    return result
  },
  component: ReleaseHistoryPage,
})

function ReleaseHistoryPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
  const allowed = canRelease(user)
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const queryClient = useQueryClient()
  const page = search.page ?? 1
  const [text, setText] = useState(search.q ?? '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const path = jobsPath({ q: search.q, kind: search.kind, status: search.status, page })
  const list = useQuery({ queryKey: [jobListKey, path], queryFn: () => request<JobPage>(path), enabled: allowed, placeholderData: keepPreviousData, refetchInterval: 15_000 })

  const filter = (patch: HistorySearch) => void navigate({ search: (previous) => ({ ...previous, ...patch, page: undefined }), replace: true })
  useEffect(() => {
    const timer = setTimeout(() => { if ((search.q ?? '') !== text.trim()) filter({ q: text.trim() || undefined }) }, 300)
    return () => clearTimeout(timer)
  })
  async function retry(job: Job) {
    setBusy(true); setError('')
    try {
      const next = await request<Job>(`/api/releases/jobs/${job.id}/retry`, {})
      void queryClient.invalidateQueries({ queryKey: [jobListKey] })
      void navigate({ to: '/release', search: { job: next.job_id || next.id } })
    } catch (cause) { setError(releaseErrorText(cause, zh)) }
    finally { setBusy(false) }
  }
  if (!allowed) return <Alert><Lock aria-hidden="true" /><AlertDescription>{pick(zh, '需要操作员或管理员角色才能查看释放记录。', 'An Operator or Admin role is required to see the release history.')}</AlertDescription></Alert>
  const filterSelect = (key: 'kind' | 'status', label: string, options: [string, string][]) =>
    <Select value={search[key] ?? ANY} onValueChange={(value) => filter({ [key]: value === ANY ? undefined : value })}>
      <SelectTrigger aria-label={label} className="w-auto min-w-36"><SelectValue /></SelectTrigger>
      <SelectContent><SelectGroup>
        <SelectItem value={ANY}>{label}</SelectItem>
        {options.map(([value, name]) => <SelectItem key={value} value={value}>{name}</SelectItem>)}
      </SelectGroup></SelectContent>
    </Select>
  const filtered = filterKeys.some((key) => search[key])
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '释放记录', 'Release history')} description={admin
      ? pick(zh, '所有人的查询与释放任务，最新的在前。点击 IP 查看详情和原始输出。', 'Every query and release job, newest first. Select an IP for its details and raw output.')
      : pick(zh, '你的查询与释放任务，最新的在前。点击 IP 查看详情和原始输出。', 'Your query and release jobs, newest first. Select an IP for its details and raw output.')}>
      <Button asChild><Link to="/release"><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '新的释放', 'New release')}</Link></Button>
    </PageHeader>
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={pick(zh, '搜索 IP 或交换机', 'Search IP or switch')} placeholder={pick(zh, '搜索 IP 或交换机', 'Search IP or switch')} value={text} onChange={(event) => setText(event.target.value)} className="w-full sm:w-64" />
      {filterSelect('kind', pick(zh, '全部类型', 'All types'), kinds.map((kind) => [kind, jobKindLabel(kind, zh)]))}
      {filterSelect('status', pick(zh, '全部状态', 'Any status'), statuses.map((status) => [status, statusLabel(status, zh)]))}
    </div>
    {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
    <ErrorNote error={list.error} zh={zh} />
    {list.data
      ? list.data.items.length
        ? <JobTable jobs={list.data.items} zh={zh} admin={admin} busy={busy} onRetry={(job) => void retry(job)} />
        : <Empty>{filtered ? pick(zh, '没有符合筛选条件的任务。', 'No job matches these filters.') : pick(zh, '还没有释放任务。', 'No release jobs yet.')}</Empty>
      : list.isPending ? <Skeleton className="h-64 rounded-xl" /> : null}
    {list.data ? <Pager page={page} total={list.data.total} pageSize={list.data.page_size} zh={zh} onPage={(next) => void navigate({ search: (previous) => ({ ...previous, page: next > 1 ? next : undefined }) })} /> : null}
  </main>
}
