import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query'
import { CircleAlert, Download, FolderTree, HardDriveDownload, MapPin, Pencil, Plus, PlugZap, Power, PowerOff, Upload, X } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { opsRequest, type SwitchPage, type TaskKind } from '@/features/ops/api'
import { DeviceDetailSheet } from '@/features/ops/device-detail'
import { GroupManager } from '@/features/ops/group-manager'
import { FacetFilter } from '@/features/ops/facet-filter'
import { ImportDialog } from '@/features/ops/import-dialog'
import { AdminOnly, Empty, ErrorNote, LoginBadge, PageHeader, Pager, ReachabilityBadge, ToneBadge, Time, isAdmin, kindLabel, pick, useZh } from '@/features/ops/ui'
import {
  AssetEditDialog, AssetRequestError, DisableAssetDialog, NO_GROUP, callAssets, defaultPort, portFollowing, profileFollowing, statusBody, vendorName, vendorProfiles, vendors, type EditTarget, type Inventory,
} from '@/features/release/asset-editors'
import { errorText, serverText } from '@/features/release/messages'
import { useRequiredFields } from '@/features/release/required-fields'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

type DeviceSearch = { q?: string; group?: string; vendor?: string; site?: string; reachability?: string; backup?: string; networks?: string; state?: string; page?: number }
const filterKeys = ['q', 'group', 'vendor', 'site', 'reachability', 'backup', 'networks', 'state'] as const
// Disabled switches stay out of the list unless asked for; "all" shows both.
const ALL_STATES = 'all'

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

/** The list and export query for the current filters; the state defaults to enabled switches. */
function filterParams(search: DeviceSearch): URLSearchParams {
  const params = new URLSearchParams()
  for (const key of filterKeys) if (search[key] && key !== 'state') params.set(key, search[key])
  if (search.state !== ALL_STATES) params.set('state', search.state ?? 'enabled')
  return params
}

function CreateSwitchDialog({ open, zh, inventory, onClose, onCreated }: { open: boolean; zh: boolean; inventory?: Inventory; onClose: () => void; onCreated: (handlesRelease: boolean) => void }) {
  const empty = { name: '', management_ip: '', credential_id: '', command_profile_id: '', vendor: 'huawei', model: '', protocol: 'ssh', port: defaultPort('ssh'), group_id: '', site: '', description: '' }
  const [form, setForm] = useState(empty)
  const [handlesRelease, setHandlesRelease] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const label = {
    name: pick(zh, '名称', 'Name'), ip: pick(zh, '管理 IPv4', 'Management IPv4'), credential: pick(zh, '凭据', 'Credential'), profile: pick(zh, '命令模板', 'Command profile'),
  }
  // The profile list holds the vendor's profiles; a vendor with one active profile has it chosen already.
  const profiles = vendorProfiles(inventory?.profiles, form.vendor).filter((profile) => profile.is_active)
  const profileID = form.command_profile_id || (profiles.length === 1 ? profiles[0].id : '')
  const required = useRequiredFields(zh, [
    { id: 'new-name', label: label.name, value: form.name },
    { id: 'new-management_ip', label: label.ip, value: form.management_ip },
    { id: 'new-credential', label: label.credential, value: form.credential_id, choose: true },
    { id: 'new-profile', label: label.profile, value: profileID, choose: true },
  ])
  const set = (key: keyof typeof empty, value: string) => setForm((current) => ({
    ...current, [key]: value, ...portFollowing(current, key, value), ...profileFollowing(current, key, value, inventory?.profiles),
  }))
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!required.check()) return
    if (form.port && !/^\d+$/.test(form.port)) { setError(pick(zh, '端口必须是 1–65535 之间的数字。', 'Port must be a number from 1 to 65535.')); return }
    setBusy(true); setError('')
    try {
      // On its protocol's default port a switch stores none, so it follows the protocol if that changes.
      const port = form.port.trim() ? Number(form.port) : null
      await callAssets('switches', { ...form, command_profile_id: profileID, management_ip: form.management_ip.trim(), port: port === Number(defaultPort(form.protocol)) ? null : port, handles_release: handlesRelease })
      setForm(empty); setHandlesRelease(false); required.reset(); onCreated(handlesRelease)
    } catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }
  const text = (key: keyof typeof empty, fieldLabel: string, props: React.InputHTMLAttributes<HTMLInputElement> = {}) =>
    <Field data-invalid={required.invalid(`new-${key}`)}>
      <FieldLabel htmlFor={`new-${key}`}>{fieldLabel}</FieldLabel>
      <Input id={`new-${key}`} value={form[key]} onChange={(event) => set(key, event.target.value)} autoComplete="off" {...props} {...required.control(`new-${key}`)} />
      {required.message(`new-${key}`)}
    </Field>
  const choose = (id: string, key: keyof typeof empty, fieldLabel: string, items: { value: string; label: string }[], placeholder?: string, current = form[key]) =>
    <Field data-invalid={required.invalid(id)}>
      <FieldLabel htmlFor={id}>{fieldLabel}</FieldLabel>
      <Select value={current} onValueChange={(value) => set(key, value)}>
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
            {choose('new-vendor', 'vendor', pick(zh, '厂商', 'Vendor'), vendors.map((v) => ({ value: v, label: vendorName(v, zh) })))}
            {text('model', pick(zh, '型号（可选）', 'Model (optional)'))}
            {choose('new-profile', 'command_profile_id', label.profile, profiles.map((p) => ({ value: p.id, label: p.name })),
              profiles.length ? pick(zh, '请选择', 'Choose…') : pick(zh, '这个厂商还没有命令模板', 'No command profile for this vendor yet'), profileID)}
            {choose('new-credential', 'credential_id', label.credential, inventory?.credentials.filter((c) => c.is_active).map((c) => ({ value: c.id, label: c.name })) ?? [], pick(zh, '请选择', 'Choose…'))}
            {choose('new-protocol', 'protocol', pick(zh, '登录协议', 'Login protocol'), [{ value: 'ssh', label: 'SSH' }, { value: 'telnet', label: 'Telnet' }])}
            <Field>
              <FieldLabel htmlFor="new-port">{pick(zh, '端口', 'Port')}</FieldLabel>
              <Input id="new-port" inputMode="numeric" className="font-mono" placeholder={defaultPort(form.protocol)} value={form.port} onChange={(event) => set('port', event.target.value)} />
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
          <Field orientation="horizontal">
            <Checkbox id="new-handles-release" checked={handlesRelease} onCheckedChange={(checked) => setHandlesRelease(checked === true)} />
            <div className="flex flex-col gap-1">
              <FieldLabel htmlFor="new-handles-release" className="font-normal">{pick(zh, '负责解绑', 'Handles releases')}</FieldLabel>
              <FieldDescription>{pick(zh, '网关交换机打开：添加后马上从它的配置读取网段，解绑时按网段找到它。', 'For gateway switches: its networks are read from its configuration right after it is added, and releases find it by them.')}</FieldDescription>
            </div>
          </Field>
        </FieldGroup>
        {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
        <DialogFooter><Button type="button" variant="outline" onClick={onClose}>{pick(zh, '取消', 'Cancel')}</Button><Button type="submit" disabled={busy}>{busy ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '添加', 'Add')}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}

/** A confirmation for a change to many switches; `run` happens only on the action button. */
type Pending = { title: string; description: string; action: string; run: () => void }

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
  const [managingGroups, setManagingGroups] = useState(false)
  const [settingSite, setSettingSite] = useState(false)
  const [site, setSite] = useState('')
  const [pending, setPending] = useState<Pending | null>(null)
  const params = filterParams(search)
  const exportPath = `/api/ops/switches/export?${params}`
  params.set('page', String(page))
  const listKey = params.toString()
  // A selection belongs to the page it was made on; new filters or another page start empty.
  const [selection, setSelection] = useState<{ key: string; ids: Set<string> }>({ key: '', ids: new Set() })
  const selected = selection.key === listKey ? selection.ids : new Set<string>()
  const list = useQuery({ queryKey: ['ops-switches', listKey], queryFn: () => opsRequest<SwitchPage>(`switches?${listKey}`), enabled: admin, placeholderData: keepPreviousData, refetchInterval: 10_000 })
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
  /** One change to every selected switch. `hint` follows the result; `keep` leaves the switches selected for a next step. */
  async function bulk(change: Record<string, unknown>, { hint, keep }: { hint?: string; keep?: boolean } = {}) {
    setBusy(true)
    try {
      const result = await callAssets<{ changed: number; unchanged: number; missing: number }>('switches/bulk', { ids: [...selected], ...change })
      notifySuccess(pick(zh, `已修改 ${result.changed} 台${result.unchanged ? `，${result.unchanged} 台原本就是这样` : ''}。`, `${result.changed} switch${result.changed === 1 ? '' : 'es'} changed${result.unchanged ? `; ${result.unchanged} already matched` : ''}.`) + (hint ? ` ${hint}` : ''))
      if (!keep) setSelection({ key: '', ids: new Set() })
    } catch (cause) { failed(cause) }
    finally { await refresh(); setBusy(false) }
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

  const rows = list.data?.items ?? []
  const groups = inventory.data?.groups ?? []
  const credentials = inventory.data?.credentials.filter((credential) => credential.is_active) ?? []
  const groupName = groups.find((group) => group.id === search.group)?.name
  const scopeLabel = groupName ? pick(zh, `分组“${groupName}”中所有启用的交换机`, `every enabled switch in group “${groupName}”`) : pick(zh, '所有启用的交换机', 'every enabled switch')
  const count = selected.size
  const chosen = pick(zh, `选中的 ${count} 台交换机`, `the ${count} selected switch${count === 1 ? '' : 'es'}`)
  const allChosen = rows.length > 0 && rows.every((row) => selected.has(row.id))
  const choose = (ids: string[], on: boolean) => {
    const next = new Set(selected)
    for (const id of ids) if (on) next.add(id); else next.delete(id)
    setSelection({ key: listKey, ids: next })
  }
  const facet = (key: Exclude<(typeof filterKeys)[number], 'q' | 'state'>, label: string, anyLabel: string, options: [string, string][]) =>
    <FacetFilter label={label} anyLabel={anyLabel} value={search[key]} options={options} onChange={(value) => filter({ [key]: value })} />
  const filtered = filterKeys.some((key) => search[key])
  const sites = list.data?.sites ?? []
  return <main className="mx-auto flex w-full max-w-7xl flex-col gap-6">
    <PageHeader title={pick(zh, '设备', 'Devices')}>
      <Button type="button" variant="outline" disabled={busy} onClick={() => setPending({
        title: pick(zh, `测试${scopeLabel}的连接？`, `Test the connection to ${scopeLabel}?`),
        description: pick(zh, '工作进程会逐台登录一次再退出，确认能连上、账号密码正确，不执行任何命令。大量设备会排队分批执行。', 'The worker signs in to each switch once and out again, checking it answers and its credential works; no command is sent. Large inventories are worked through in batches.'),
        action: pick(zh, '开始', 'Start'), run: () => void run('login_check', search.group && search.group !== 'none' ? { scope: 'group', group_id: search.group } : { scope: 'all' }),
      })}><PlugZap aria-hidden="true" data-icon="inline-start" />{pick(zh, '全部测试', 'Test all')}</Button>
      <Button type="button" variant="outline" disabled={busy} onClick={() => setPending({
        title: pick(zh, `备份${scopeLabel}的配置？`, `Back up ${scopeLabel}?`),
        description: pick(zh, '工作进程会逐台登录并读取运行配置，只读取、不修改。大量设备会排队分批执行。', 'The worker signs in to each switch and reads its running configuration; nothing is changed. Large inventories are worked through in batches.'),
        action: pick(zh, '开始', 'Start'), run: () => void run('config_backup', search.group && search.group !== 'none' ? { scope: 'group', group_id: search.group } : { scope: 'all' }),
      })}><HardDriveDownload aria-hidden="true" data-icon="inline-start" />{pick(zh, '全部备份', 'Back up all')}</Button>
      <Separator orientation="vertical" className="mx-1 hidden h-6 sm:block" />
      <Button asChild variant="outline"><a href={exportPath} download><Download aria-hidden="true" data-icon="inline-start" />{pick(zh, '导出', 'Export')}</a></Button>
      <Button type="button" variant="outline" onClick={() => setManagingGroups(true)}><FolderTree aria-hidden="true" data-icon="inline-start" />{pick(zh, '管理分组', 'Groups')}</Button>
      <Button type="button" variant="outline" onClick={() => setImporting(true)}><Upload aria-hidden="true" data-icon="inline-start" />{pick(zh, '批量导入', 'Import CSV')}</Button>
      <Button type="button" onClick={() => setCreating(true)}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '添加交换机', 'Add switch')}</Button>
    </PageHeader>
    <div className="flex flex-wrap items-center gap-2">
      <Input aria-label={pick(zh, '搜索名称、IP 或校区', 'Search name, IP or campus')} placeholder={pick(zh, '搜索名称、IP 或校区', 'Search name, IP or campus')} value={text} onChange={(event) => setText(event.target.value)} className="h-9 w-full sm:w-56" />
      {facet('group', pick(zh, '分组', 'Group'), pick(zh, '全部分组', 'All groups'), [['none', pick(zh, '未分组', 'No group')], ...groups.map((g): [string, string] => [g.id, g.name])])}
      {sites.length ? facet('site', pick(zh, '校区', 'Campus'), pick(zh, '全部校区', 'All campuses'), sites.map((value): [string, string] => [value, value])) : null}
      {facet('vendor', pick(zh, '厂商', 'Vendor'), pick(zh, '全部厂商', 'All vendors'), vendors.map((v) => [v, vendorName(v, zh)]))}
      {facet('reachability', pick(zh, '连接', 'Connection'), pick(zh, '全部', 'Any'), [['reachable', pick(zh, '在线', 'Reachable')], ['unreachable', pick(zh, '不可达', 'Unreachable')], ['login_failed', pick(zh, '登录失败', 'Sign-in fails')], ['unknown', pick(zh, '未检测', 'Not checked')]])}
      {facet('backup', pick(zh, '备份', 'Backup'), pick(zh, '全部', 'Any'), [['ok', pick(zh, '备份正常', 'Backed up')], ['failed', pick(zh, '最近一次失败', 'Last backup failed')], ['overdue', pick(zh, '逾期（超过 7 天）', 'Overdue (over 7 days)')], ['never', pick(zh, '从未成功', 'Never succeeded')]])}
      {facet('networks', pick(zh, '解绑', 'Releases'), pick(zh, '全部', 'Any'), [['release', pick(zh, '负责解绑', 'Handles releases')], ['none', pick(zh, '负责解绑但没有网段', 'Handles releases, no network')], ['other', pick(zh, '不负责解绑', 'Handles no releases')]])}
      <FacetFilter label={pick(zh, '启用状态', 'Status')} defaultValue="enabled" value={search.state} onChange={(value) => filter({ state: value })}
        options={[['enabled', pick(zh, '仅启用', 'Enabled')], ['disabled', pick(zh, '仅停用', 'Disabled')], [ALL_STATES, pick(zh, '启用和停用', 'Enabled and disabled')]]} />
      {filtered ? <Button type="button" variant="ghost" size="sm" className="px-2.5" onClick={() => { setText(''); void navigate({ search: {}, replace: true }) }}>
        {pick(zh, '重置', 'Reset')}<X aria-hidden="true" data-icon="inline-end" /></Button> : null}
    </div>
    {count ? <div role="region" aria-label={pick(zh, '批量操作', 'Bulk actions')} className="flex flex-wrap items-center gap-2 rounded-xl border bg-muted/40 px-3 py-2">
      <span className="text-sm font-medium tabular-nums">{pick(zh, `已选 ${count} 台`, `${count} selected`)}</span>
      <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void run('login_check', { scope: 'switches', switch_ids: [...selected] })}><PlugZap aria-hidden="true" data-icon="inline-start" />{pick(zh, '测试连接', 'Test connection')}</Button>
      <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void run('config_backup', { scope: 'switches', switch_ids: [...selected] })}><HardDriveDownload aria-hidden="true" data-icon="inline-start" />{pick(zh, '立即备份', 'Back up now')}</Button>
      <Select value="" onValueChange={(value) => {
        const name = value === NO_GROUP ? '' : groups.find((group) => group.id === value)?.name ?? ''
        setPending({
          title: name ? pick(zh, `把${chosen}移动到分组“${name}”？`, `Move ${chosen} to group “${name}”?`) : pick(zh, `把${chosen}移出分组？`, `Remove ${chosen} from their groups?`),
          description: pick(zh, '只改变分组，不影响解绑和设备连接。', 'Only the group changes; releases and connections are unaffected.'),
          action: pick(zh, '移动', 'Move'), run: () => void bulk({ group_id: value === NO_GROUP ? '' : value }),
        })
      }}>
        <SelectTrigger aria-label={pick(zh, '移动到分组', 'Move to group')} className="h-9 w-auto min-w-32" disabled={busy}><SelectValue placeholder={pick(zh, '移动到分组…', 'Move to group…')} /></SelectTrigger>
        <SelectContent><SelectGroup>
          {groups.map((group) => <SelectItem key={group.id} value={group.id}>{group.name}</SelectItem>)}
          <SelectItem value={NO_GROUP}>{pick(zh, '移出分组', 'No group')}</SelectItem>
        </SelectGroup></SelectContent>
      </Select>
      <Select value="" onValueChange={(value) => {
        const name = credentials.find((credential) => credential.id === value)?.name ?? ''
        setPending({
          title: pick(zh, `把${chosen}的凭据换成“${name}”？`, `Switch ${chosen} to credential “${name}”?`),
          description: pick(zh, '以后解绑、测试和备份都用这条凭据登录。这里只改记录，不会连接交换机。', 'Releases, tests and backups sign in with this credential from now on. Only the record changes; no switch is contacted.'),
          action: pick(zh, '更换', 'Switch'),
          run: () => void bulk({ credential_id: value }, { keep: true, hint: pick(zh, '可以点“测试连接”确认能登录。', 'Use Test connection to check they sign in.') }),
        })
      }}>
        <SelectTrigger aria-label={pick(zh, '更换凭据', 'Change credential')} className="h-9 w-auto min-w-32" disabled={busy || !credentials.length}><SelectValue placeholder={pick(zh, '更换凭据…', 'Change credential…')} /></SelectTrigger>
        <SelectContent><SelectGroup>
          {credentials.map((credential) => <SelectItem key={credential.id} value={credential.id}>{credential.name}</SelectItem>)}
        </SelectGroup></SelectContent>
      </Select>
      <Select value="" onValueChange={(value) => setPending(value === 'on' ? {
        title: pick(zh, `把${chosen}设为负责解绑？`, `Make ${chosen} handle releases?`),
        description: pick(zh, '系统会马上登录读取它们的配置，按接口地址生成网段，之后每天更新。只用于网关交换机，不修改交换机。', 'Their configurations are read right away and their interface addresses become networks, updated every day. Meant for gateway switches; nothing on the switches changes.'),
        action: pick(zh, '设为负责解绑', 'Handle releases'), run: () => void bulk({ handles_release: true }),
      } : {
        title: pick(zh, `取消${chosen}的负责解绑？`, `Stop ${chosen} handling releases?`),
        description: pick(zh, '它们的网段会被移除，网段内的地址解绑时不会再找到这些交换机。', 'Their networks are removed, so releases of addresses in them no longer find these switches.'),
        action: pick(zh, '取消负责解绑', 'Stop handling releases'), run: () => void bulk({ handles_release: false }),
      })}>
        <SelectTrigger aria-label={pick(zh, '负责解绑', 'Release role')} className="h-9 w-auto min-w-28" disabled={busy}><SelectValue placeholder={pick(zh, '负责解绑…', 'Release role…')} /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value="on">{pick(zh, '设为负责解绑', 'Handle releases')}</SelectItem>
          <SelectItem value="off">{pick(zh, '取消负责解绑', 'Stop handling releases')}</SelectItem>
        </SelectGroup></SelectContent>
      </Select>
      <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => { setSite(''); setSettingSite(true) }}><MapPin aria-hidden="true" data-icon="inline-start" />{pick(zh, '设置校区', 'Set campus')}</Button>
      <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void bulk({ is_enabled: true })}><Power aria-hidden="true" data-icon="inline-start" />{pick(zh, '启用', 'Enable')}</Button>
      <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => setPending({
        title: pick(zh, `停用${chosen}？`, `Disable ${chosen}?`),
        description: pick(zh, '它们网段内的地址将不再解析到这些交换机，排队中的任务会直接失败，不会连接交换机。停用的设备不参与任何检测和备份。', 'Addresses in their networks stop resolving to them, and their queued jobs fail without contacting the switch. Disabled switches take part in no check or backup.'),
        action: pick(zh, '停用', 'Disable'), run: () => void bulk({ is_enabled: false }),
      })}><PowerOff aria-hidden="true" data-icon="inline-start" />{pick(zh, '停用', 'Disable')}</Button>
      <Button type="button" variant="ghost" size="sm" className="ml-auto" onClick={() => setSelection({ key: '', ids: new Set() })}><X aria-hidden="true" data-icon="inline-start" />{pick(zh, '取消选择', 'Clear selection')}</Button>
    </div> : null}
    <ErrorNote error={list.error} zh={zh} />
    {list.data && !rows.length
      ? <Empty>{filtered ? pick(zh, '没有符合筛选条件的交换机。', 'No switch matches these filters.') : pick(zh, '还没有启用的交换机。可以逐台添加，或用 CSV 批量导入。', 'No enabled switches yet. Add them one by one or import a CSV.')}</Empty>
      : <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
        <Table>
          <TableHeader><TableRow>
            <TableHead className="w-10"><Checkbox aria-label={pick(zh, '选择本页全部', 'Select this page')} checked={allChosen ? true : count ? 'indeterminate' : false} disabled={!rows.length}
              onCheckedChange={(checked) => choose(rows.map((row) => row.id), checked === true)} /></TableHead>
            <TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '管理地址', 'Management')}</TableHead><TableHead>{pick(zh, '厂商 / 分组', 'Vendor / group')}</TableHead>
            <TableHead>{pick(zh, '连接', 'Connection')}</TableHead><TableHead>{pick(zh, '最近成功备份', 'Last backup')}</TableHead><TableHead>{pick(zh, '解绑网段', 'Networks')}</TableHead><TableHead>{pick(zh, '合规', 'Compliance')}</TableHead>
            <TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>
            {rows.map((row) => {
              const target: EditTarget = { kind: 'switches', item: row }
              const backupError = serverText(row.config_error, zh)
              return <TableRow key={row.id} className="cursor-pointer" data-state={selected.has(row.id) ? 'selected' : undefined} onClick={() => setDetail(row.id)}>
                <TableCell onClick={(event) => event.stopPropagation()}><Checkbox aria-label={pick(zh, `选择 ${row.name}`, `Select ${row.name}`)} checked={selected.has(row.id)} onCheckedChange={(checked) => choose([row.id], checked === true)} /></TableCell>
                <TableCell className="max-w-52"><div className="flex flex-wrap items-center gap-2"><span className={row.is_enabled ? 'font-medium' : 'font-medium text-muted-foreground'}>{row.name}</span>{row.is_enabled ? null : <ToneBadge kind="muted">{pick(zh, '已停用', 'Disabled')}</ToneBadge>}</div>
                  {row.site || row.description ? <p className="truncate text-xs text-muted-foreground">{[row.site, row.description].filter(Boolean).join(' · ')}</p> : null}</TableCell>
                <TableCell className="whitespace-nowrap"><span className="font-mono text-xs">{row.management_ip}</span><p className="text-xs text-muted-foreground">{protocolName(row.protocol)} {row.port ?? (row.protocol === 'ssh' ? 22 : 23)}</p></TableCell>
                <TableCell className="whitespace-nowrap text-sm">{vendorName(row.vendor, zh)}{row.model ? <span className="text-muted-foreground"> · {row.model}</span> : null}<p className="text-xs text-muted-foreground">{row.group_name ?? pick(zh, '未分组', 'No group')}</p></TableCell>
                <TableCell className="whitespace-nowrap"><div className="flex flex-wrap gap-1"><ReachabilityBadge reachable={row.reachable} latency={row.reachability_latency_ms} error={row.reachability_error} zh={zh} /><LoginBadge reachable={row.reachable} ok={row.login_ok} error={row.login_error} zh={zh} /></div>
                  <p className="mt-0.5 text-xs text-muted-foreground"><Time value={row.reachability_checked_at} zh={zh} /></p></TableCell>
                <TableCell>{row.config_succeeded_at ? <Time value={row.config_succeeded_at} zh={zh} className="text-sm" /> : <ToneBadge kind="warn">{pick(zh, '从未成功', 'Never')}</ToneBadge>}
                  {backupError ? <p className="text-xs whitespace-nowrap text-destructive" title={backupError}>{pick(zh, '最近一次失败', 'Last attempt failed')}</p> : null}</TableCell>
                <TableCell>{!row.handles_release ? <span className="text-xs text-muted-foreground" title={pick(zh, '不负责解绑', 'Handles no releases')}>—</span>
                  : row.networks ? <span className="text-sm tabular-nums" title={row.networks_error ? serverText(row.networks_error, zh) : undefined}>{row.networks}{row.networks_error ? <span className="ml-1 text-destructive">!</span> : null}</span>
                    : <ToneBadge kind="warn">{row.networks_checked_at ? pick(zh, '无', 'None') : pick(zh, '读取中', 'Reading')}</ToneBadge>}</TableCell>
                <TableCell>{row.compliance_failed ? <ToneBadge kind="bad">{pick(zh, `${row.compliance_failed} 项未通过`, `${row.compliance_failed} failing`)}</ToneBadge> : <span className="text-xs text-muted-foreground">—</span>}</TableCell>
                <TableCell className="text-right"><div className="flex justify-end gap-0.5" onClick={(event) => event.stopPropagation()}>
                  <Button type="button" variant="ghost" size="sm" className="size-8 p-0" disabled={busy || !row.is_enabled} title={pick(zh, '测试连接：登录一次再退出', 'Test connection: sign in and out once')} aria-label={`${pick(zh, '测试连接', 'Test connection')} ${row.name}`} onClick={() => void run('login_check', { scope: 'switches', switch_ids: [row.id] })}><PlugZap aria-hidden="true" /></Button>
                  <Button type="button" variant="ghost" size="sm" className="size-8 p-0" disabled={busy || !row.is_enabled} title={pick(zh, '立即备份', 'Back up now')} aria-label={`${pick(zh, '立即备份', 'Back up now')} ${row.name}`} onClick={() => void run('config_backup', { scope: 'switches', switch_ids: [row.id] })}><HardDriveDownload aria-hidden="true" /></Button>
                  <Button type="button" variant="ghost" size="sm" className="size-8 p-0" disabled={busy} title={pick(zh, '编辑', 'Edit')} aria-label={`${pick(zh, '编辑', 'Edit')} ${row.name}`} onClick={() => setEditing(target)}><Pencil aria-hidden="true" /></Button>
                  <Button type="button" variant="ghost" size="sm" className="size-8 p-0" disabled={busy} title={row.is_enabled ? pick(zh, '停用', 'Disable') : pick(zh, '启用', 'Enable')} aria-label={`${row.is_enabled ? pick(zh, '停用', 'Disable') : pick(zh, '启用', 'Enable')} ${row.name}`} onClick={() => row.is_enabled ? setDisabling(target) : void setEnabled(target, true)}>{row.is_enabled ? <PowerOff aria-hidden="true" /> : <Power aria-hidden="true" />}</Button>
                </div></TableCell>
              </TableRow>
            })}
          </TableBody>
        </Table>
      </div>}
    {list.data ? <Pager page={page} total={list.data.total} pageSize={list.data.page_size} zh={zh} onPage={(next) => void navigate({ search: (previous) => ({ ...previous, page: next > 1 ? next : undefined }) })} /> : null}
    <DeviceDetailSheet switchId={detail} zh={zh} busy={busy} inventory={inventory.data} onClose={() => setDetail(null)} onRun={(kind, id) => void run(kind, { scope: 'switches', switch_ids: [id] })} />
    <AssetEditDialog target={editing} inventory={inventory.data} zh={zh} onClose={() => setEditing(null)} onSave={saveEdit} />
    <DisableAssetDialog target={disabling} inventory={inventory.data} zh={zh} onCancel={() => setDisabling(null)} onConfirm={(target) => void setEnabled(target, false)} />
    <CreateSwitchDialog open={creating} zh={zh} inventory={inventory.data} onClose={() => setCreating(false)} onCreated={(handlesRelease) => { setCreating(false); notifySuccess(handlesRelease ? pick(zh, '交换机已添加，正在读取它的网段。', 'Switch added; its networks are being read.') : pick(zh, '交换机已添加。', 'Switch added.')); void refresh() }} />
    <ImportDialog open={importing} zh={zh} inventory={inventory.data} onClose={() => setImporting(false)} onImported={() => void refresh()} />
    <Dialog open={managingGroups} onOpenChange={setManagingGroups}>
      <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{pick(zh, '管理分组', 'Groups')}</DialogTitle>
          <DialogDescription>{pick(zh, '分组用于筛选设备、限定计划任务和合规规则的范围。删除分组后，组里的设备变为未分组。', 'Groups filter devices and scope schedules and compliance rules. Deleting a group leaves its switches ungrouped.')}</DialogDescription>
        </DialogHeader>
        <GroupManager zh={zh} inventory={inventory.data} onChanged={refresh} />
      </DialogContent>
    </Dialog>
    <Dialog open={settingSite} onOpenChange={setSettingSite}>
      <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="sm:max-w-md">
        <form className="flex flex-col gap-6" noValidate onSubmit={(event) => { event.preventDefault(); setSettingSite(false); void bulk({ site: site.trim() }) }}>
          <DialogHeader>
            <DialogTitle>{pick(zh, `设置${chosen}的校区`, `Set the campus of ${chosen}`)}</DialogTitle>
            <DialogDescription>{pick(zh, '留空则清除校区。', 'Leave it empty to clear the campus.')}</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="bulk-site">{pick(zh, '校区', 'Campus')}</FieldLabel>
            <Input id="bulk-site" list="bulk-site-options" value={site} onChange={(event) => setSite(event.target.value)} autoComplete="off" autoFocus />
            <datalist id="bulk-site-options">{sites.map((value) => <option key={value} value={value} />)}</datalist>
          </Field>
          <DialogFooter><Button type="button" variant="outline" onClick={() => setSettingSite(false)}>{pick(zh, '取消', 'Cancel')}</Button><Button type="submit">{pick(zh, '设置', 'Set')}</Button></DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
    <AlertDialog open={pending !== null} onOpenChange={(open) => { if (!open) setPending(null) }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{pending?.title}</AlertDialogTitle>
          <AlertDialogDescription>{pending?.description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{pick(zh, '取消', 'Cancel')}</AlertDialogCancel>
          <AlertDialogAction onClick={() => pending?.run()}>{pending?.action}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </main>
}
