import { createFileRoute } from '@tanstack/react-router'
import { CircleAlert, CircleCheck, Plus } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { FacetFilter } from '@/features/ops/facet-filter'
import { AdminOnly, Empty, ErrorNote, PageHeader, Pager, ToneBadge, pick } from '@/features/ops/ui'
import { type EditTarget, type SwitchAsset } from '@/features/release/asset-editors'
import { AssetActions, CreateAssetDialog, useAssetPage } from '@/features/release/asset-page'
import { errorText } from '@/features/release/messages'
import { contains, ipv4, prefixOf, resolve, usable, type NetworkRow } from '@/features/release/networks'
import { useRequiredFields } from '@/features/release/required-fields'

export const Route = createFileRoute('/_authenticated/networks')({ component: NetworksPage })

const PAGE_SIZE = 50
const blank = { switch_id: '', cidr: '', vlan: '', description: '', is_validated: false }

function NetworksPage() {
  const page = useAssetPage()
  const { zh, labels, data } = page
  const [query, setQuery] = useState('')
  const [state, setState] = useState<string | undefined>()
  const [pageNumber, setPageNumber] = useState(1)
  const [creating, setCreating] = useState(false)
  const [form, setForm] = useState(blank)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const label = { switch: pick(zh, '所属交换机', 'Switch'), cidr: 'IPv4 CIDR' }
  const required = useRequiredFields(zh, [
    { id: 'network-switch', label: label.switch, value: form.switch_id, choose: true },
    { id: 'network-cidr', label: label.cidr, value: form.cidr },
  ])
  if (!page.admin) return <AdminOnly zh={zh} />

  async function save() {
    if (!required.check()) return
    setSaving(true); setError('')
    try { await page.create('networks', { ...form, cidr: form.cidr.trim() }); setCreating(false); setForm(blank); required.reset() }
    catch (cause) { setError(errorText(cause, zh)) }
    finally { setSaving(false) }
  }

  const switches = new Map((data?.switches ?? []).map((item) => [item.id, item] as const))
  const all: NetworkRow[] = (data?.networks ?? []).map((network) => ({ network, owner: switches.get(network.switch_id), bits: prefixOf(network.cidr)?.bits ?? 0 }))
  // An address shows the networks containing it, most specific first; other text matches loosely.
  const address = ipv4(query)
  const text = query.trim().toLowerCase()
  const matched = address !== null
    ? all.filter((row) => contains(row.network.cidr, address)).sort((a, b) => b.bits - a.bits)
    : all.filter((row) => !text || [row.network.cidr, row.owner?.name, row.owner?.management_ip, row.network.vlan, row.network.description]
      .some((value) => value?.toLowerCase().includes(text)))
  const rows = matched.filter((row) => !state
    || state === 'usable' && usable(row)
    || state === 'unvalidated' && !row.network.is_validated
    || state === 'disabled' && (!row.network.is_enabled || !row.owner?.is_enabled))
  const shown = rows.slice((pageNumber - 1) * PAGE_SIZE, pageNumber * PAGE_SIZE)
  const resolution = address !== null ? resolve(matched) : null
  const switchText = (item?: SwitchAsset) => item ? `${item.name}（${item.management_ip}）` : '—'

  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '网段', 'Networks')}>
      <Button type="button" onClick={() => { setError(''); setCreating(true) }}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '新建网段', 'New network')}</Button>
    </PageHeader>
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={pick(zh, '搜索 IP、网段或交换机', 'Search an IP, network or switch')} placeholder={pick(zh, '输入 IP 查它归哪台交换机，或搜索网段、交换机', 'Type an IP to see its switch, or search networks and switches')}
        value={query} onChange={(event) => { setQuery(event.target.value); setPageNumber(1) }} className="h-9 w-full sm:w-96" autoComplete="off" />
      <FacetFilter label={pick(zh, '状态', 'Status')} anyLabel={pick(zh, '全部', 'All')} value={state} onChange={(value) => { setState(value); setPageNumber(1) }}
        options={[['usable', pick(zh, '可用', 'Usable')], ['unvalidated', pick(zh, '未验证', 'Not validated')], ['disabled', pick(zh, '已停用', 'Disabled')]]} />
    </div>
    {resolution ? <Alert variant={resolution.kind === 'switch' ? 'default' : 'destructive'}>
      {resolution.kind === 'switch' ? <CircleCheck aria-hidden="true" /> : <CircleAlert aria-hidden="true" />}
      <AlertDescription>{resolution.kind === 'switch'
        ? pick(zh, `解绑 ${query.trim()} 时会找 ${switchText(resolution.row.owner)}，依据网段 ${resolution.row.network.cidr}。`,
          `Releasing ${query.trim()} goes to ${switchText(resolution.row.owner)}, by network ${resolution.row.network.cidr}.`)
        : resolution.kind === 'none'
          ? pick(zh, `没有可用的网段包含 ${query.trim()}，解绑这个地址会失败。需要为它登记一个已启用、已验证的网段。`,
            `No usable network contains ${query.trim()}, so releasing it fails. It needs an enabled, validated network.`)
          : pick(zh, `有 ${resolution.rows.length} 个同样精确的网段分属不同交换机，解绑会停下来，需要先修正网段数据。`,
            `${resolution.rows.length} equally specific networks belong to different switches, so releasing stops until the networks are corrected.`)}</AlertDescription>
    </Alert> : null}
    <ErrorNote error={page.inventory.error} zh={zh} />
    {data && !rows.length
      ? <Empty>{query || state ? pick(zh, '没有符合条件的网段。', 'No network matches.') : labels.empty.networks}</Empty>
      : <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{pick(zh, '网段', 'Network')}</TableHead><TableHead>{label.switch}</TableHead><TableHead>VLAN</TableHead>
            <TableHead>{pick(zh, '描述', 'Description')}</TableHead><TableHead>{pick(zh, '状态', 'Status')}</TableHead><TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>{shown.map((row) => {
            const target: EditTarget = { kind: 'networks', item: row.network }
            const chosen = resolution?.kind === 'switch' && resolution.row.network.id === row.network.id
            return <TableRow key={row.network.id} data-state={chosen ? 'selected' : undefined}>
              <TableCell className="whitespace-nowrap font-mono text-sm">{row.network.cidr}</TableCell>
              <TableCell className="whitespace-nowrap text-sm">{row.owner?.name ?? '—'}<p className="font-mono text-xs text-muted-foreground">{row.owner?.management_ip}</p></TableCell>
              <TableCell className="text-sm">{row.network.vlan ?? <span className="text-muted-foreground">—</span>}</TableCell>
              <TableCell className="max-w-56 truncate text-sm text-muted-foreground" title={row.network.description ?? undefined}>{row.network.description ?? '—'}</TableCell>
              <TableCell><div className="flex flex-wrap gap-1">
                {usable(row) ? <ToneBadge kind="good">{pick(zh, '可用', 'Usable')}</ToneBadge> : null}
                {row.network.is_validated ? null : <ToneBadge kind="warn">{labels.unvalidated}</ToneBadge>}
                {row.network.is_enabled ? null : <ToneBadge kind="muted">{labels.disabled}</ToneBadge>}
                {row.owner && !row.owner.is_enabled ? <ToneBadge kind="muted">{pick(zh, '交换机已停用', 'Switch disabled')}</ToneBadge> : null}
              </div></TableCell>
              <TableCell><AssetActions title={row.network.cidr} on={row.network.is_enabled} busy={page.busy} labels={labels} onEdit={() => page.edit(target)} onToggle={() => page.toggle(target)} /></TableCell>
            </TableRow>
          })}</TableBody>
        </Table>
      </div>}
    {data && rows.length ? <Pager page={pageNumber} total={rows.length} pageSize={PAGE_SIZE} zh={zh} onPage={setPageNumber} /> : null}
    <CreateAssetDialog open={creating} zh={zh} title={pick(zh, '新建网段', 'New network')} error={error} saving={saving} onClose={() => setCreating(false)} onSubmit={() => void save()}
      description={pick(zh, '网段决定一个 IP 由哪台交换机负责；只有已启用并已验证的网段参与解绑时的查找。', 'A network decides which switch owns an IP; only enabled, validated networks are used to find it when releasing.')}>
      <FieldGroup>
        <Field data-invalid={required.invalid('network-switch')}>
          <FieldLabel htmlFor="network-switch">{label.switch}</FieldLabel>
          <Select value={form.switch_id} onValueChange={(value) => setForm({ ...form, switch_id: value })}>
            <SelectTrigger id="network-switch" {...required.control('network-switch')}><SelectValue placeholder={labels.choose} /></SelectTrigger>
            <SelectContent><SelectGroup>{[...switches.values()].sort((a, b) => a.name.localeCompare(b.name)).map((item) =>
              <SelectItem key={item.id} value={item.id}>{item.name}（{item.management_ip}）</SelectItem>)}</SelectGroup></SelectContent>
          </Select>
          {required.message('network-switch')}
        </Field>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field data-invalid={required.invalid('network-cidr')}>
            <FieldLabel htmlFor="network-cidr">{label.cidr}</FieldLabel>
            <Input id="network-cidr" className="font-mono" placeholder="10.0.0.0/24" value={form.cidr} onChange={(event) => setForm({ ...form, cidr: event.target.value })} {...required.control('network-cidr')} autoComplete="off" />
            {required.message('network-cidr')}
          </Field>
          <Field>
            <FieldLabel htmlFor="network-vlan">{labels.vlan}</FieldLabel>
            <Input id="network-vlan" value={form.vlan} onChange={(event) => setForm({ ...form, vlan: event.target.value })} autoComplete="off" />
          </Field>
        </div>
        <Field>
          <FieldLabel htmlFor="network-description">{labels.description}</FieldLabel>
          <Input id="network-description" value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} autoComplete="off" />
        </Field>
        <Field orientation="horizontal">
          <Checkbox id="network-validated" checked={form.is_validated} onCheckedChange={(checked) => setForm({ ...form, is_validated: checked === true })} />
          <FieldLabel htmlFor="network-validated" className="font-normal">{labels.validated}</FieldLabel>
        </Field>
      </FieldGroup>
    </CreateAssetDialog>
    {page.dialogs}
  </main>
}
