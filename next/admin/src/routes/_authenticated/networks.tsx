import { Link, createFileRoute } from '@tanstack/react-router'
import { CircleAlert, CircleCheck } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { AdminOnly, Empty, ErrorNote, PageHeader, Pager, ToneBadge, pick } from '@/features/ops/ui'
import { type SwitchAsset } from '@/features/release/asset-editors'
import { useAssetPage } from '@/features/release/asset-page'
import { contains, ipv4, prefixOf, resolve, usable, type NetworkRow } from '@/features/release/networks'

export const Route = createFileRoute('/_authenticated/networks')({ component: NetworksPage })

const PAGE_SIZE = 50

/**
 * The networks releases are routed by. They are read from the release switches' configuration, so
 * this page only shows them and looks an address up; nothing here changes them.
 */
function NetworksPage() {
  const page = useAssetPage()
  const { zh, data } = page
  const [query, setQuery] = useState('')
  const [pageNumber, setPageNumber] = useState(1)
  if (!page.admin) return <AdminOnly zh={zh} />

  const switches = new Map((data?.switches ?? []).map((item) => [item.id, item] as const))
  const all: NetworkRow[] = (data?.networks ?? []).map((network) => ({ network, owner: switches.get(network.switch_id), bits: prefixOf(network.cidr)?.bits ?? 0 }))
  // An address shows the networks containing it, most specific first; other text matches loosely.
  const address = ipv4(query)
  const text = query.trim().toLowerCase()
  const rows = address !== null
    ? all.filter((row) => contains(row.network.cidr, address)).sort((a, b) => b.bits - a.bits)
    : all.filter((row) => !text || [row.network.cidr, row.owner?.name, row.owner?.management_ip, row.network.interface, row.network.vlan, row.network.description]
      .some((value) => value?.toLowerCase().includes(text)))
  const shown = rows.slice((pageNumber - 1) * PAGE_SIZE, pageNumber * PAGE_SIZE)
  const resolution = address !== null ? resolve(rows) : null
  const switchText = (item?: SwitchAsset) => item ? `${item.name}（${item.management_ip}）` : '—'
  const releaseSwitches = (data?.switches ?? []).filter((item) => item.handles_release && item.is_enabled)
  const unread = releaseSwitches.filter((item) => item.networks_error || !item.networks_succeeded_at).length

  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '网段', 'Networks')} />
    <Input aria-label={pick(zh, '搜索 IP、网段、交换机或接口', 'Search an IP, network, switch or interface')} placeholder={pick(zh, '输入 IP 查它归哪台交换机，或搜索网段、交换机、接口', 'Type an IP to see its switch, or search networks, switches and interfaces')}
      value={query} onChange={(event) => { setQuery(event.target.value); setPageNumber(1) }} className="h-9 w-full sm:w-96" autoComplete="off" />
    {resolution ? <Alert variant={resolution.kind === 'switch' ? 'default' : 'destructive'}>
      {resolution.kind === 'switch' ? <CircleCheck aria-hidden="true" /> : <CircleAlert aria-hidden="true" />}
      <AlertDescription>{resolution.kind === 'switch'
        ? pick(zh, `解绑 ${query.trim()} 时会找 ${switchText(resolution.row.owner)}，依据网段 ${resolution.row.network.cidr}。`,
          `Releasing ${query.trim()} goes to ${switchText(resolution.row.owner)}, by network ${resolution.row.network.cidr}.`)
        : resolution.kind === 'none'
          ? pick(zh, `没有负责解绑的交换机配置了包含 ${query.trim()} 的网段，解绑这个地址会停下来。确认它的网关交换机已打开“负责解绑”，并且网段已经读到。`,
            `No release switch has a network containing ${query.trim()}, so releasing it stops. Check that its gateway switch handles releases and that its networks have been read.`)
          : pick(zh, `有 ${resolution.rows.length} 台交换机配置了同样精确的网段，解绑会停下来。改正交换机上的配置后，在设备详情里刷新网段。`,
            `${resolution.rows.length} switches have equally specific networks, so releasing stops. Correct the switch configuration, then refresh the networks in the device details.`)}</AlertDescription>
    </Alert> : null}
    {unread ? <Alert>
      <CircleAlert aria-hidden="true" />
      <AlertDescription>{pick(zh, `${unread} 台负责解绑的交换机还没读到网段，或最近一次读取失败。`, `${unread} release switch${unread === 1 ? ' has' : 'es have'} no networks read yet, or the last read failed.`)}{' '}
        <Link to="/devices" search={{ networks: 'release' }} className="underline underline-offset-4">{pick(zh, '去设备页查看', 'See the devices')}</Link></AlertDescription>
    </Alert> : null}
    <ErrorNote error={page.inventory.error} zh={zh} />
    {data && !rows.length
      ? <Empty>{query
        ? pick(zh, '没有符合条件的网段。', 'No network matches.')
        : pick(zh, '还没有网段。在设备页给网关交换机打开“负责解绑”后，会自动从它们的配置里读取网段。', 'No networks yet. Turn on “Handles releases” for gateway switches on the devices page, and their networks are read from their configuration.')}</Empty>
      : <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{pick(zh, '网段', 'Network')}</TableHead><TableHead>{pick(zh, '交换机', 'Switch')}</TableHead><TableHead>{pick(zh, '接口', 'Interface')}</TableHead>
            <TableHead>{pick(zh, '描述', 'Description')}</TableHead><TableHead>{pick(zh, '状态', 'Status')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>{shown.map((row) => {
            const chosen = resolution?.kind === 'switch' && resolution.row.network.id === row.network.id
            return <TableRow key={row.network.id} data-state={chosen ? 'selected' : undefined}>
              <TableCell className="whitespace-nowrap font-mono text-sm">{row.network.cidr}</TableCell>
              <TableCell className="whitespace-nowrap text-sm">{row.owner?.name ?? '—'}<p className="font-mono text-xs text-muted-foreground">{row.owner?.management_ip}</p></TableCell>
              <TableCell className="whitespace-nowrap text-sm">{row.network.interface ?? <span className="text-muted-foreground">{pick(zh, '手工登记', 'Typed in')}</span>}</TableCell>
              <TableCell className="max-w-56 truncate text-sm text-muted-foreground" title={row.network.description ?? undefined}>{row.network.description ?? '—'}</TableCell>
              <TableCell><div className="flex flex-wrap gap-1">
                {usable(row) ? <ToneBadge kind="good">{pick(zh, '可用', 'Usable')}</ToneBadge> : null}
                {row.owner && !row.owner.is_enabled ? <ToneBadge kind="muted">{pick(zh, '交换机已停用', 'Switch disabled')}</ToneBadge> : null}
                {row.network.interface ? null : <ToneBadge kind="warn">{pick(zh, '待读取配置后替换', 'Replaced by the next read')}</ToneBadge>}
              </div></TableCell>
            </TableRow>
          })}</TableBody>
        </Table>
      </div>}
    {data && rows.length ? <Pager page={pageNumber} total={rows.length} pageSize={PAGE_SIZE} zh={zh} onPage={setPageNumber} /> : null}
  </main>
}
