import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Lock, Plus } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { Empty, ErrorNote, PageHeader, Pager, isAdmin, pick, useZh } from '@/features/ops/ui'
import { JobTable } from '@/features/release/job-lists'
import { canRelease, jobListKey, jobsPath, request, statusLabel, type JobPage } from '@/features/release/jobs'

type HistorySearch = { q?: string; kind?: 'lookup'; status?: string; page?: number }
const filterKeys = ['q', 'status'] as const
// A filter set to everything travels as this select value and leaves the search parameter out.
const ANY = 'any'
const statuses = ['succeeded', 'no_static_binding', 'no_record', 'failed', 'timeout', 'needs_manual_confirmation', 'queued', 'running']
// A finished lookup shows what it found; its status only says whether it got an answer.
const lookupStatuses = ['succeeded', 'failed', 'timeout', 'queued', 'running']

export const Route = createFileRoute('/_authenticated/releases')({
  validateSearch: (search: Record<string, unknown>): HistorySearch => {
    const result: HistorySearch = {}
    for (const key of filterKeys) {
      if (typeof search[key] === 'string' && search[key]) result[key] = search[key]
    }
    if (search.kind === 'lookup') result.kind = 'lookup'
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
  const page = search.page ?? 1
  const [text, setText] = useState(search.q ?? '')
  // Releases, or lookups; queries left from the old two-step flow stay out, though their own pages still open.
  const lookups = search.kind === 'lookup'
  const path = jobsPath({ q: search.q, kind: lookups ? 'lookup' : 'release', status: search.status, page })
  const list = useQuery({ queryKey: [jobListKey, path], queryFn: () => request<JobPage>(path), enabled: allowed, placeholderData: keepPreviousData, refetchInterval: 15_000 })

  const filter = (patch: HistorySearch) => void navigate({ search: (previous) => ({ ...previous, ...patch, page: undefined }), replace: true })
  useEffect(() => {
    const timer = setTimeout(() => { if ((search.q ?? '') !== text.trim()) filter({ q: text.trim() || undefined }) }, 300)
    return () => clearTimeout(timer)
  })
  if (!allowed) return <Alert><Lock aria-hidden="true" /><AlertDescription>{pick(zh, '需要操作员或管理员角色才能查看解绑记录。', 'An Operator or Admin role is required to see the release history.')}</AlertDescription></Alert>
  const filterSelect = (key: 'status', label: string, options: [string, string][]) =>
    <Select value={search[key] ?? ANY} onValueChange={(value) => filter({ [key]: value === ANY ? undefined : value })}>
      <SelectTrigger aria-label={label} className="w-auto min-w-36"><SelectValue /></SelectTrigger>
      <SelectContent><SelectGroup>
        <SelectItem value={ANY}>{label}</SelectItem>
        {options.map(([value, name]) => <SelectItem key={value} value={value}>{name}</SelectItem>)}
      </SelectGroup></SelectContent>
    </Select>
  const filtered = filterKeys.some((key) => search[key])
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '解绑记录', 'Release history')}>
      <Button asChild><Link to="/release"><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '发起解绑', 'New release')}</Link></Button>
    </PageHeader>
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={pick(zh, '搜索 IP 或交换机', 'Search IP or switch')} placeholder={pick(zh, '搜索 IP 或交换机', 'Search IP or switch')} value={text} onChange={(event) => setText(event.target.value)} className="w-full sm:w-64" />
      <Select value={lookups ? 'lookup' : 'release'} onValueChange={(value) => filter({ kind: value === 'lookup' ? 'lookup' : undefined, status: undefined })}>
        <SelectTrigger aria-label={pick(zh, '类型', 'Kind')} className="w-auto min-w-28"><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value="release">{pick(zh, '解绑', 'Releases')}</SelectItem>
          <SelectItem value="lookup">{pick(zh, '查询', 'Lookups')}</SelectItem>
        </SelectGroup></SelectContent>
      </Select>
      {filterSelect('status', pick(zh, '全部状态', 'Any status'), lookups
        ? lookupStatuses.map((status) => [status, status === 'succeeded' ? pick(zh, '已查到结果', 'Answered') : statusLabel(status, zh)])
        : statuses.map((status) => [status, statusLabel(status, zh)]))}
    </div>
    <ErrorNote error={list.error} zh={zh} />
    {list.data
      ? list.data.items.length
        ? <JobTable jobs={list.data.items} zh={zh} admin={admin} reasons />
        : <Empty>{lookups
          ? filtered ? pick(zh, '没有符合筛选条件的查询。', 'No lookup matches these filters.') : pick(zh, '还没有查询记录。在解绑控制台输入地址后点“查询”。', 'No lookups yet. Enter an address in the release console and choose Query.')
          : filtered ? pick(zh, '没有符合筛选条件的解绑。', 'No release matches these filters.') : pick(zh, '还没有解绑记录。', 'No releases yet.')}</Empty>
      : list.isPending ? <Skeleton className="h-64 rounded-xl" /> : null}
    {list.data ? <Pager page={page} total={list.data.total} pageSize={list.data.page_size} zh={zh} onPage={(next) => void navigate({ search: (previous) => ({ ...previous, page: next > 1 ? next : undefined }) })} /> : null}
  </main>
}
