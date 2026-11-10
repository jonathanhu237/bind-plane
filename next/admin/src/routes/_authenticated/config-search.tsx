import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Eye, Search } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { opsRequest, type ConfigSearch } from '@/features/ops/api'
import { highlightParts } from '@/features/ops/config-search'
import { ConfigDialog } from '@/features/ops/device-detail'
import { FacetFilter } from '@/features/ops/facet-filter'
import { AdminOnly, Empty, ErrorNote, PageHeader, Time, isAdmin, pick, useZh } from '@/features/ops/ui'
import { callAssets, vendorName, vendors, type Inventory } from '@/features/release/asset-editors'

type SearchState = { text?: string; mode?: 'regex'; group?: string; vendor?: string }

export const Route = createFileRoute('/_authenticated/config-search')({
  validateSearch: (search: Record<string, unknown>): SearchState => {
    const result: SearchState = {}
    for (const key of ['text', 'group', 'vendor'] as const) {
      if (typeof search[key] === 'string' && search[key]) result[key] = search[key]
    }
    if (search.mode === 'regex') result.mode = 'regex'
    return result
  },
  component: ConfigSearchPage,
})

// Searches that come up often, offered before the first one.
const examples = ['local-user', 'telnet server enable', 'snmp-agent community', 'ntp-service unicast-server']

function ConfigSearchPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const [text, setText] = useState(search.text ?? '')
  const [regex, setRegex] = useState(search.mode === 'regex')
  const [viewing, setViewing] = useState<number | null>(null)
  const params = new URLSearchParams()
  if (search.text) params.set('text', search.text)
  if (search.mode) params.set('mode', search.mode)
  if (search.group) params.set('group', search.group)
  if (search.vendor) params.set('vendor', search.vendor)
  // Each search is recorded in the audit log, so a result is reused rather than fetched again.
  const query = useQuery({
    queryKey: ['config-search', params.toString()], enabled: admin && Boolean(search.text),
    queryFn: () => opsRequest<ConfigSearch>(`config-search?${params}`), staleTime: 60_000, refetchOnWindowFocus: false, retry: false,
  })
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin, staleTime: 60_000 })
  if (!admin) return <AdminOnly zh={zh} />
  const go = (next: Partial<SearchState>) => void navigate({ search: { ...search, ...next }, replace: true })
  const submit = (value = text, asRegex = regex) => {
    setText(value)
    go({ text: value.trim() || undefined, mode: asRegex ? 'regex' : undefined })
  }
  const data = query.data
  const groups = inventory.data?.groups ?? []
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '配置搜索', 'Configuration search')} />
    <p className="-mt-3 max-w-3xl text-sm text-muted-foreground">{pick(zh,
      '在每台交换机最近一次备份的配置里查找文字，不登录交换机。普通搜索不区分大小写；正则表达式按 RE2 语法，区分大小写，可在开头加 (?i) 忽略大小写。每次搜索都会记入审计日志。',
      'Finds text in the newest backed-up configuration of every switch, without signing in to any. Plain searches ignore case; patterns are RE2 and match case unless they start with (?i). Every search is recorded in the audit log.')}</p>
    <form className="flex flex-wrap items-center gap-2" onSubmit={(event) => { event.preventDefault(); submit() }}>
      <Input aria-label={pick(zh, '要查找的文字', 'Text to find')} placeholder={regex ? pick(zh, '正则表达式，例如 ^interface Vlanif\\d+$', 'A pattern, such as ^interface Vlanif\\d+$') : pick(zh, '例如 local-user、telnet server enable', 'Such as local-user or telnet server enable')}
        value={text} onChange={(event) => setText(event.target.value)} className="h-9 w-full font-mono sm:w-96" />
      <Field orientation="horizontal" className="w-auto">
        <Checkbox id="search-regex" checked={regex} onCheckedChange={(checked) => setRegex(checked === true)} />
        <FieldLabel htmlFor="search-regex" className="font-normal">{pick(zh, '正则表达式', 'Pattern')}</FieldLabel>
      </Field>
      <FacetFilter label={pick(zh, '分组', 'Group')} anyLabel={pick(zh, '全部分组', 'All groups')} value={search.group} onChange={(value) => go({ group: value })}
        options={[['none', pick(zh, '未分组', 'No group')], ...groups.map((group): [string, string] => [group.id, group.name])]} />
      <FacetFilter label={pick(zh, '厂商', 'Vendor')} anyLabel={pick(zh, '全部厂商', 'All vendors')} value={search.vendor} onChange={(value) => go({ vendor: value })}
        options={vendors.map((vendor): [string, string] => [vendor, vendorName(vendor, zh)])} />
      <Button type="submit" disabled={query.isFetching}><Search aria-hidden="true" data-icon="inline-start" />{query.isFetching ? pick(zh, '正在搜索…', 'Searching…') : pick(zh, '搜索', 'Search')}</Button>
    </form>
    <ErrorNote error={query.error} zh={zh} />
    {!search.text ? <div className="flex flex-col gap-2">
      <p className="text-sm text-muted-foreground">{pick(zh, '常用的搜索：', 'Common searches:')}</p>
      <div className="flex flex-wrap gap-1.5">{examples.map((example) => <Button key={example} type="button" variant="outline" size="sm" className="font-mono" onClick={() => { setRegex(false); submit(example, false) }}>{example}</Button>)}</div>
    </div> : null}
    {data ? <section aria-label={pick(zh, '搜索结果', 'Results')} className="flex flex-col gap-4">
      <p className="text-sm" aria-live="polite">{pick(zh,
        `搜索了 ${data.searched} 台有备份的交换机，${data.matched} 台匹配，共 ${data.lines} 行。`,
        `Searched ${data.searched} backed-up switch${data.searched === 1 ? '' : 'es'}: ${data.matched} matched, ${data.lines} line${data.lines === 1 ? '' : 's'} in all.`)}
        {data.without_backup ? <span className="text-muted-foreground">{pick(zh, ` 另有 ${data.without_backup} 台还没有备份，未搜索。`, ` ${data.without_backup} without a backup were not searched.`)}</span> : null}
        {data.truncated ? <span className="text-warning">{pick(zh, ` 只列出按名称排序的前 ${data.results.length} 台，请把搜索写得更具体。`, ` Only the first ${data.results.length} by name are listed; narrow the search.`)}</span> : null}</p>
      {data.results.length ? <ul className="flex flex-col gap-3">{data.results.map((result) => <li key={result.switch_id} className="overflow-hidden rounded-xl border">
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-b bg-muted/40 px-4 py-2 text-sm">
          <span className="font-medium">{result.switch_name}</span>
          <span className="font-mono text-xs text-muted-foreground">{result.management_ip}</span>
          {result.group_name ? <span className="text-xs text-muted-foreground">{result.group_name}</span> : null}
          <span className="text-xs text-muted-foreground">{pick(zh, '备份于', 'Backed up')} <Time value={result.captured_at} zh={zh} /></span>
          <span className="ml-auto text-xs tabular-nums">{pick(zh, `${result.count} 行`, `${result.count} line${result.count === 1 ? '' : 's'}`)}</span>
          <Button type="button" variant="ghost" size="sm" onClick={() => setViewing(result.backup_id)}><Eye aria-hidden="true" data-icon="inline-start" />{pick(zh, '查看配置', 'View configuration')}</Button>
        </div>
        <ol className="flex flex-col gap-0.5 px-4 py-2 font-mono text-xs">{result.matches.map((match) => <li key={match.line} className="flex gap-3">
          <span className="w-10 shrink-0 select-none text-right text-muted-foreground tabular-nums">{match.line}</span>
          <span className="min-w-0 whitespace-pre-wrap break-all">{match.section ? <span className="text-muted-foreground">{match.section} › </span> : null}
            {highlightParts(match.section ? match.text.trimStart() : match.text, search.text ?? '', search.mode === 'regex').map((part, index) => part.hit
              ? <mark key={index} className="rounded-sm bg-chart-1/25 text-foreground">{part.text}</mark> : <span key={index}>{part.text}</span>)}</span>
        </li>)}
          {result.count > result.matches.length ? <li className="pl-[3.25rem] text-muted-foreground">{pick(zh, `另有 ${result.count - result.matches.length} 行未显示，可查看完整配置。`, `${result.count - result.matches.length} more line(s); view the configuration for all.`)}</li> : null}
        </ol>
      </li>)}</ul> : <Empty>{pick(zh, `没有交换机的配置包含“${search.text}”。`, `No configuration contains “${search.text}”.`)}</Empty>}
    </section> : null}
    <ConfigDialog backupId={viewing} zh={zh} onClose={() => setViewing(null)} />
  </main>
}
