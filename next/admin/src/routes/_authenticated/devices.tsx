import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, HardDriveDownload, Pencil, Plus, Power, PowerOff, Upload, Wifi } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { opsRequest, type OpsSwitch, type Page, type TaskKind } from '@/features/ops/api'
import { DeviceDetailSheet } from '@/features/ops/device-detail'
import { ImportDialog } from '@/features/ops/import-dialog'
import { AdminOnly, Empty, ErrorNote, PageHeader, Pager, ReachabilityBadge, ToneBadge, Time, isAdmin, kindLabel, pick, useZh } from '@/features/ops/ui'
import {
  AssetEditDialog, AssetRequestError, DisableAssetDialog, NO_GROUP, callAssets, statusBody, vendorName, vendors, type EditTarget, type Inventory,
} from '@/features/release/asset-editors'
import { errorText, serverText } from '@/features/release/messages'
import { useRequiredFields } from '@/features/release/required-fields'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

type DeviceSearch = { q?: string; group?: string; vendor?: string; reachability?: string; backup?: string; state?: string; page?: number }
const filterKeys = ['q', 'group', 'vendor', 'reachability', 'backup', 'state'] as const
// A filter set to everything travels as this select value and leaves the search parameter out.
const ANY = 'any'

export const Route = createFileRoute('/_authenticated/devices')({
  validateSearch: (search: Record<string, unknown>): DeviceSearch => {
    const result: DeviceSearch = {}
    for (const key of filterKeys) {
      if (typeof search[key] === 'string' && search[key]) result[key] = search[key]
    }
    const page = Number(search.page)
    if (Number.isInteger(page) && page > 1) result.page = page
    return result
  },
  component: DevicesPage,
})

const protocolName = (protocol: string) => (protocol === 'ssh' ? 'SSH' : 'Telnet')

function CreateSwitchDialog({ open, zh, inventory, onClose, onCreated }: { open: boolean; zh: boolean; inventory?: Inventory; onClose: () => void; onCreated: () => void }) {
  const empty = { name: '', management_ip: '', credential_id: '', command_profile_id: '', vendor: 'huawei', model: '', protocol: 'ssh', port: '', group_id: '', site: '', description: '' }
  const [form, setForm] = useState(empty)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const label = {
    name: pick(zh, '名称', 'Name'), ip: pick(zh, '管理 IPv4', 'Management IPv4'), credential: pick(zh, '凭据', 'Credential'), profile: pick(zh, '命令模板', 'Command profile'),
  }
  const required = useRequiredFields(zh, [
    { id: 'new-name', label: label.name, value: form.name },
    { id: 'new-management_ip', label: label.ip, value: form.management_ip },
    { id: 'new-credential', label: label.credential, value: form.credential_id, choose: true },
    { id: 'new-profile', label: label.profile, value: form.command_profile_id, choose: true },
  ])
  const set = (key: keyof typeof empty, value: string) => setForm((current) => ({ ...current, [key]: value }))
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!required.check()) return
    if (form.port && !/^\d+$/.test(form.port)) { setError(pick(zh, '端口必须是 1–65535 之间的数字。', 'Port must be a number from 1 to 65535.')); return }
    setBusy(true); setError('')
    try {
      await callAssets('switches', { ...form, management_ip: form.management_ip.trim(), port: form.port ? Number(form.port) : null })
      setForm(empty); required.reset(); onCreated()
    } catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }
  const text = (key: keyof typeof empty, fieldLabel: string, props: React.InputHTMLAttributes<HTMLInputElement> = {}) =>
    <Field data-invalid={required.invalid(`new-${key}`)}>
      <FieldLabel htmlFor={`new-${key}`}>{fieldLabel}</FieldLabel>
      <Input id={`new-${key}`} value={form[key]} onChange={(event) => set(key, event.target.value)} autoComplete="off" {...props} {...required.control(`new-${key}`)} />
      {required.message(`new-${key}`)}
    </Field>
  const choose = (id: string, key: keyof typeof empty, fieldLabel: string, items: { value: string; label: string }[], placeholder?: string) =>
    <Field data-invalid={required.invalid(id)}>
      <FieldLabel htmlFor={id}>{fieldLabel}</FieldLabel>
      <Select value={form[key]} onValueChange={(value) => set(key, value)}>
        <SelectTrigger id={id} {...required.control(id)}><SelectValue placeholder={placeholder} /></SelectTrigger>
        <SelectContent><SelectGroup>{items.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}</SelectGroup></SelectContent>
      </Select>
      {required.message(id)}
    </Field>
  return <Dialog open={open} onOpenChange={(next) => { if (!next) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
      <form className="flex flex-col gap-6" noValidate onSubmit={(event) => void submit(event)}>
        <DialogHeader>
          <DialogTitle>{pick(zh, '添加交换机', 'Add switch')}</DialogTitle>
          <DialogDescription>{pick(zh, '只添加你被授权管理的设备。凭据和命令模板在“资产配置”中维护。', 'Only add devices you are authorized to manage. Credentials and command profiles live under Asset settings.')}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          {text('name', label.name)}
          {text('management_ip', label.ip, { inputMode: 'decimal', className: 'font-mono' })}
          <div className="grid gap-4 sm:grid-cols-2">
            {choose('new-credential', 'credential_id', label.credential, inventory?.credentials.filter((c) => c.is_active).map((c) => ({ value: c.id, label: c.name })) ?? [], pick(zh, '请选择', 'Choose…'))}
            {choose('new-profile', 'command_profile_id', label.profile, inventory?.profiles.filter((p) => p.is_active).map((p) => ({ value: p.id, label: p.name })) ?? [], pick(zh, '请选择', 'Choose…'))}
            {choose('new-vendor', 'vendor', pick(zh, '厂商', 'Vendor'), vendors.map((v) => ({ value: v, label: vendorName(v, zh) })))}
            {text('model', pick(zh, '型号（可选）', 'Model (optional)'))}
            {choose('new-protocol', 'protocol', pick(zh, '登录协议', 'Login protocol'), [{ value: 'ssh', label: 'SSH' }, { value: 'telnet', label: 'Telnet' }])}
            <Field>
              <FieldLabel htmlFor="new-port">{pick(zh, '端口（可选）', 'Port (optional)')}</FieldLabel>
              <Input id="new-port" inputMode="numeric" className="font-mono" placeholder={form.protocol === 'ssh' ? '22' : '23'} aria-describedby="new-port-hint" value={form.port} onChange={(event) => set('port', event.target.value)} />
              <FieldDescription id="new-port-hint">{pick(zh, '留空则使用协议默认端口。', 'Leave empty for the protocol default.')}</FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="new-group">{pick(zh, '分组', 'Group')}</FieldLabel>
              <Select value={form.group_id || NO_GROUP} onValueChange={(value) => set('group_id', value === NO_GROUP ? '' : value)}>
                <SelectTrigger id="new-group"><SelectValue /></SelectTrigger>
                <SelectContent><SelectGroup>
                  <SelectItem value={NO_GROUP}>{pick(zh, '不分组', 'No group')}</SelectItem>
                  {inventory?.groups.map((g) => <SelectItem key={g.id} value={g.id}>{g.name}</SelectItem>)}
                </SelectGroup></SelectContent>
              </Select>
            </Field>
            {text('site', pick(zh, '站点或校区（可选）', 'Site or campus (optional)'))}
          </div>
          {text('description', pick(zh, '描述（可选）', 'Description (optional)'))}
        </FieldGroup>
        {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
        <DialogFooter><Button type="button" variant="outline" onClick={onClose}>{pick(zh, '取消', 'Cancel')}</Button><Button type="submit" disabled={busy}>{busy ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '添加', 'Add')}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}

function DevicesPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const { t } = useTranslation()
  const admin = isAdmin(user)
  const search = Route.useSearch()
  const navigate = Route.useNavigate()
  const queryClient = useQueryClient()
  const page = search.page ?? 1
  const [text, setText] = useState(search.q ?? '')
  const [busy, setBusy] = useState(false)
  const [detail, setDetail] = useState<string | null>(null)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [disabling, setDisabling] = useState<EditTarget | null>(null)
  const [creating, setCreating] = useState(false)
  const [importing, setImporting] = useState(false)
  const [bulk, setBulk] = useState<TaskKind | null>(null)
  const params = new URLSearchParams()
  for (const key of filterKeys) if (search[key]) params.set(key, search[key])
  params.set('page', String(page))
  const list = useQuery({ queryKey: ['ops-switches', params.toString()], queryFn: () => opsRequest<Page<OpsSwitch>>(`switches?${params}`), enabled: admin, placeholderData: keepPreviousData, refetchInterval: 10_000 })
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin, staleTime: 60_000 })

  const filter = (patch: DeviceSearch) => void navigate({ search: (previous) => ({ ...previous, ...patch, page: undefined }), replace: true })
  useEffect(() => {
    const timer = setTimeout(() => { if ((search.q ?? '') !== text.trim()) filter({ q: text.trim() || undefined }) }, 300)
    return () => clearTimeout(timer)
  })
  const failed = (cause: unknown) => notifyRequestError(cause, t, { description: errorText(cause, zh) })
  async function refresh() {
    await Promise.all([list.refetch(), queryClient.invalidateQueries({ queryKey: ['release-assets'] }), queryClient.invalidateQueries({ queryKey: ['ops-switch'] })])
  }
  async function run(kind: TaskKind, body: Record<string, unknown>) {
    setBusy(true)
    try {
      const result = await opsRequest<{ queued: number; skipped: number }>('tasks', { kind, ...body })
      notifySuccess(pick(zh, `${kindLabel(kind, zh)}已排队：${result.queued} 台${result.skipped ? `，${result.skipped} 台已在队列中被跳过` : ''}。`, `${kindLabel(kind, zh)} queued for ${result.queued} switch${result.queued === 1 ? '' : 'es'}${result.skipped ? `; ${result.skipped} already pending were skipped` : ''}.`))
      setTimeout(() => void refresh(), 4000)
    } catch (cause) { failed(cause) }
    finally { setBusy(false) }
  }
  async function saveEdit(path: string, body: Record<string, unknown>) {
    try { await callAssets(path, body, 'PATCH') }
    catch (error) { if (error instanceof AssetRequestError && error.status === 409) await refresh(); throw error }
    setEditing(null); notifySuccess(pick(zh, '已保存。', 'Saved.')); await refresh()
  }
  async function setEnabled(target: EditTarget, on: boolean) {
    setBusy(true)
    try { await callAssets(`${target.kind}/${target.item.id}`, statusBody(target, on), 'PATCH'); notifySuccess(on ? pick(zh, '已启用。', 'Enabled.') : pick(zh, '已停用。', 'Disabled.')) }
    catch (cause) { failed(cause) }
    finally { await refresh(); setBusy(false) }
  }
  if (!admin) return <AdminOnly zh={zh} />
  const groupName = inventory.data?.groups.find((group) => group.id === search.group)?.name
  const scopeLabel = groupName ? pick(zh, `分组“${groupName}”中所有启用的交换机`, `every enabled switch in group “${groupName}”`) : pick(zh, '所有启用的交换机', 'every enabled switch')
  const filterSelect = (key: (typeof filterKeys)[number], label: string, options: [string, string][]) =>
    <Select value={search[key] ?? ANY} onValueChange={(value) => filter({ [key]: value === ANY ? undefined : value })}>
      <SelectTrigger aria-label={label} className="w-auto min-w-36"><SelectValue /></SelectTrigger>
      <SelectContent><SelectGroup>
        <SelectItem value={ANY}>{label}</SelectItem>
        {options.map(([value, name]) => <SelectItem key={value} value={value}>{name}</SelectItem>)}
      </SelectGroup></SelectContent>
    </Select>
  const filtered = filterKeys.some((key) => search[key])
  return <main className="mx-auto flex w-full max-w-7xl flex-col gap-6">
    <PageHeader title={pick(zh, '设备', 'Devices')} description={pick(zh, '交换机清单、连通性和配置备份状态。点击一行查看配置版本、对比和合规结果。', 'Switch inventory with reachability and backup status. Select a row for configuration versions, diffs and compliance results.')}>
      <Button type="button" variant="outline" onClick={() => setImporting(true)}><Upload aria-hidden="true" data-icon="inline-start" />{pick(zh, '批量导入', 'Import CSV')}</Button>
      <Button type="button" onClick={() => setCreating(true)}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '添加交换机', 'Add switch')}</Button>
    </PageHeader>
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={pick(zh, '搜索名称、IP 或站点', 'Search name, IP or site')} placeholder={pick(zh, '搜索名称、IP 或站点', 'Search name, IP or site')} value={text} onChange={(event) => setText(event.target.value)} className="w-full sm:w-64" />
      {filterSelect('group', pick(zh, '全部分组', 'All groups'), inventory.data?.groups.map((g) => [g.id, g.name]) ?? [])}
      {filterSelect('vendor', pick(zh, '全部厂商', 'All vendors'), vendors.map((v) => [v, vendorName(v, zh)]))}
      {filterSelect('reachability', pick(zh, '全部连通性', 'Any reachability'), [['reachable', pick(zh, '在线', 'Reachable')], ['unreachable', pick(zh, '不可达', 'Unreachable')], ['unknown', pick(zh, '未检测', 'Not checked')]])}
      {filterSelect('backup', pick(zh, '全部备份状态', 'Any backup state'), [['ok', pick(zh, '备份正常', 'Backed up')], ['failed', pick(zh, '备份失败', 'Last backup failed')], ['never', pick(zh, '从未备份', 'Never backed up')]])}
      {filterSelect('state', pick(zh, '启用与停用', 'Enabled and disabled'), [['enabled', pick(zh, '仅启用', 'Enabled only')], ['disabled', pick(zh, '仅停用', 'Disabled only')]])}
      <div className="ml-auto flex gap-2">
        <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => setBulk('reachability')}><Wifi aria-hidden="true" data-icon="inline-start" />{pick(zh, '全部检测', 'Check all')}</Button>
        <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => setBulk('config_backup')}><HardDriveDownload aria-hidden="true" data-icon="inline-start" />{pick(zh, '全部备份', 'Back up all')}</Button>
      </div>
    </div>
    <ErrorNote error={list.error} zh={zh} />
    {list.data && !list.data.items.length
      ? <Empty>{filtered ? pick(zh, '没有符合筛选条件的交换机。', 'No switch matches these filters.') : pick(zh, '还没有交换机。可以逐台添加，或用 CSV 批量导入。', 'No switches yet. Add them one by one or import a CSV.')}</Empty>
      : <div className="overflow-x-auto rounded-xl border">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '管理地址', 'Management')}</TableHead><TableHead>{pick(zh, '厂商 / 分组', 'Vendor / group')}</TableHead>
            <TableHead>{pick(zh, '连通性', 'Reachability')}</TableHead><TableHead>{pick(zh, '最近备份', 'Latest backup')}</TableHead><TableHead>{pick(zh, '合规', 'Compliance')}</TableHead>
            <TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {list.data?.items.map((row) => {
              const target: EditTarget = { kind: 'switches', item: row }
              const backupError = serverText(row.config_error, zh)
              return <TableRow key={row.id} className="cursor-pointer" onClick={() => setDetail(row.id)}>
                <TableCell className="max-w-56"><div className="flex flex-wrap items-center gap-2"><span className={row.is_enabled ? 'font-medium' : 'font-medium text-muted-foreground'}>{row.name}</span>{row.is_enabled ? null : <ToneBadge kind="muted">{pick(zh, '已停用', 'Disabled')}</ToneBadge>}</div>
                  {row.site || row.description ? <p className="truncate text-xs text-muted-foreground">{[row.site, row.description].filter(Boolean).join(' · ')}</p> : null}</TableCell>
                <TableCell className="whitespace-nowrap"><span className="font-mono text-xs">{row.management_ip}</span><p className="text-xs text-muted-foreground">{protocolName(row.protocol)} {row.port ?? (row.protocol === 'ssh' ? 22 : 23)}</p></TableCell>
                <TableCell className="whitespace-nowrap text-sm">{vendorName(row.vendor, zh)}{row.model ? <span className="text-muted-foreground"> · {row.model}</span> : null}<p className="text-xs text-muted-foreground">{row.group_name ?? pick(zh, '未分组', 'No group')}</p></TableCell>
                <TableCell className="whitespace-nowrap"><ReachabilityBadge reachable={row.reachable} latency={row.reachability_latency_ms} error={row.reachability_error} zh={zh} /><p className="mt-0.5 text-xs text-muted-foreground"><Time value={row.reachability_checked_at} zh={zh} /></p></TableCell>
                <TableCell className="max-w-56">{backupError ? <ToneBadge kind="bad" title={backupError}>{pick(zh, '失败', 'Failed')}</ToneBadge> : row.latest_backup_at ? <Time value={row.latest_backup_at} zh={zh} className="text-sm" /> : <ToneBadge kind="warn">{pick(zh, '从未备份', 'Never')}</ToneBadge>}
                  {backupError ? <p className="truncate text-xs text-muted-foreground" title={backupError}>{backupError}</p> : null}</TableCell>
                <TableCell>{row.compliance_failed ? <ToneBadge kind="bad">{pick(zh, `${row.compliance_failed} 项未通过`, `${row.compliance_failed} failing`)}</ToneBadge> : <span className="text-xs text-muted-foreground">—</span>}</TableCell>
                <TableCell className="text-right"><div className="flex justify-end gap-1" onClick={(event) => event.stopPropagation()}>
                  <Button type="button" variant="ghost" size="sm" disabled={busy || !row.is_enabled} title={pick(zh, '检测连通性', 'Check reachability')} aria-label={`${pick(zh, '检测连通性', 'Check reachability')} ${row.name}`} onClick={() => void run('reachability', { scope: 'switches', switch_ids: [row.id] })}><Wifi aria-hidden="true" /></Button>
                  <Button type="button" variant="ghost" size="sm" disabled={busy || !row.is_enabled} title={pick(zh, '立即备份', 'Back up now')} aria-label={`${pick(zh, '立即备份', 'Back up now')} ${row.name}`} onClick={() => void run('config_backup', { scope: 'switches', switch_ids: [row.id] })}><HardDriveDownload aria-hidden="true" /></Button>
                  <Button type="button" variant="ghost" size="sm" disabled={busy} title={pick(zh, '编辑', 'Edit')} aria-label={`${pick(zh, '编辑', 'Edit')} ${row.name}`} onClick={() => setEditing(target)}><Pencil aria-hidden="true" /></Button>
                  <Button type="button" variant="ghost" size="sm" disabled={busy} title={row.is_enabled ? pick(zh, '停用', 'Disable') : pick(zh, '启用', 'Enable')} aria-label={`${row.is_enabled ? pick(zh, '停用', 'Disable') : pick(zh, '启用', 'Enable')} ${row.name}`} onClick={() => row.is_enabled ? setDisabling(target) : void setEnabled(target, true)}>{row.is_enabled ? <PowerOff aria-hidden="true" /> : <Power aria-hidden="true" />}</Button>
                </div></TableCell>
              </TableRow>
            })}
          </TableBody>
        </Table>
      </div>}
    {list.data ? <Pager page={page} total={list.data.total} pageSize={list.data.page_size} zh={zh} onPage={(next) => void navigate({ search: (previous) => ({ ...previous, page: next > 1 ? next : undefined }) })} /> : null}
    <DeviceDetailSheet switchId={detail} zh={zh} busy={busy} onClose={() => setDetail(null)} onRun={(kind, id) => void run(kind, { scope: 'switches', switch_ids: [id] })} />
    <AssetEditDialog target={editing} inventory={inventory.data} zh={zh} onClose={() => setEditing(null)} onSave={saveEdit} />
    <DisableAssetDialog target={disabling} inventory={inventory.data} zh={zh} onCancel={() => setDisabling(null)} onConfirm={(target) => void setEnabled(target, false)} />
    <CreateSwitchDialog open={creating} zh={zh} inventory={inventory.data} onClose={() => setCreating(false)} onCreated={() => { setCreating(false); notifySuccess(pick(zh, '交换机已添加。', 'Switch added.')); void refresh() }} />
    <ImportDialog open={importing} zh={zh} inventory={inventory.data} onClose={() => setImporting(false)} onImported={() => void refresh()} />
    <AlertDialog open={bulk !== null} onOpenChange={(open) => { if (!open) setBulk(null) }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{bulk ? pick(zh, `对${scopeLabel}执行${kindLabel(bulk, zh)}？`, `Run a ${kindLabel(bulk, zh).toLowerCase()} on ${scopeLabel}?`) : ''}</AlertDialogTitle>
          <AlertDialogDescription>{bulk === 'config_backup' ? pick(zh, '工作进程会逐台登录并读取运行配置，只读取、不修改。大量设备会排队分批执行。', 'The worker signs in to each switch and reads its running configuration; nothing is changed. Large inventories are worked through in batches.') : pick(zh, '对每台设备的管理端口做一次 TCP 连接检测。', 'Opens one TCP connection to each switch’s management port.')}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{pick(zh, '取消', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction onClick={() => { if (bulk) void run(bulk, search.group ? { scope: 'group', group_id: search.group } : { scope: 'all' }) }}>{pick(zh, '开始', 'Start')}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </main>
}
