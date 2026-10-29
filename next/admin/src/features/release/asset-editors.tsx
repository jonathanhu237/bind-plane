import { Pencil, Power, PowerOff } from 'lucide-react'
import { useState } from 'react'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { cn } from '@/lib/utils'

export const profileRuleKeys = ['command_templates', 'parser_rules', 'login_prompt_patterns', 'prompt_patterns', 'pagination_rules', 'success_patterns', 'error_patterns'] as const
type RuleKey = (typeof profileRuleKeys)[number]
const arrayRules: ReadonlySet<string> = new Set(['success_patterns', 'error_patterns'])

export type Credential = { id: string; name: string; username: string; description: string | null; is_active: boolean; has_secret: boolean; version: number }
export type Profile = { id: string; name: string; is_active: boolean; version: number } & Record<RuleKey, unknown>
export type SwitchAsset = { id: string; name: string; management_ip: string; credential_id: string; command_profile_id: string; is_enabled: boolean; version: number }
export type NetworkAsset = { id: string; switch_id: string; cidr: string; is_enabled: boolean; is_validated: boolean; vlan: string | null; description: string | null; version: number }
export type Inventory = { credentials: Credential[]; profiles: Profile[]; switches: SwitchAsset[]; networks: NetworkAsset[] }
export type AssetKind = 'credentials' | 'profiles' | 'switches' | 'networks'
export type EditTarget =
  | { kind: 'credentials'; item: Credential }
  | { kind: 'profiles'; item: Profile }
  | { kind: 'switches'; item: SwitchAsset }
  | { kind: 'networks'; item: NetworkAsset }

export const selectClass = 'w-full rounded-md border bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50'
const ruleClass = 'w-full rounded-md border bg-background px-3 py-2 font-mono text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring'

export class AssetRequestError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

export async function callAssets<T>(path: string, body?: unknown, method = body === undefined ? 'GET' : 'POST'): Promise<T> {
  const response = await fetch(`/api/releases/admin/${path}`, {
    method, credentials: 'same-origin', cache: 'no-store',
    headers: { Accept: 'application/json', ...(body === undefined ? {} : { 'Content-Type': 'application/json' }) },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const data: unknown = await response.json().catch(() => ({}))
  if (!response.ok) throw new AssetRequestError((data as { detail?: string }).detail || `HTTP ${response.status}`, response.status)
  return data as T
}

// Server details that an admin can act on are translated; anything else is shown as sent.
const knownErrors: Record<string, [string, string]> = {
  'asset changed since it was loaded; reload and try again': [
    'Someone changed this asset after you opened it. Close the editor and open it again to edit the latest version.',
    '打开后该资产已被其他人修改。请关闭编辑框后重新打开，在最新内容上修改。',
  ],
  'change conflicts with existing assets': [
    'Another asset already uses this name or address, or a selected item no longer exists.',
    '名称或地址已被其他资产使用，或所选对象已不存在。',
  ],
}

export function assetErrorText(error: unknown, zh: boolean): string {
  if (!(error instanceof Error)) return zh ? '操作失败' : 'Request failed'
  const known = knownErrors[error.message]
  return known ? known[zh ? 1 : 0] : error.message
}

export function isOn(target: EditTarget): boolean {
  return target.kind === 'credentials' || target.kind === 'profiles' ? target.item.is_active : target.item.is_enabled
}

export function statusBody(target: EditTarget, on: boolean): Record<string, unknown> {
  return { version: target.item.version, [target.kind === 'credentials' || target.kind === 'profiles' ? 'is_active' : 'is_enabled']: on }
}

export function assetName(target: EditTarget): string {
  return target.kind === 'networks' ? target.item.cidr : target.item.name
}

export function switchesUsing(target: EditTarget, inventory?: Inventory): number {
  if (target.kind === 'credentials') return inventory?.switches.filter((item) => item.credential_id === target.item.id).length ?? 0
  if (target.kind === 'profiles') return inventory?.switches.filter((item) => item.command_profile_id === target.item.id).length ?? 0
  return 0
}

type Form = Record<string, string | boolean>

function initialForm(target: EditTarget): Form {
  switch (target.kind) {
    case 'credentials':
      return { name: target.item.name, username: target.item.username, description: target.item.description ?? '', password: '', secret: '', clear_secret: false }
    case 'profiles':
      return { name: target.item.name, ...Object.fromEntries(profileRuleKeys.map((key) => [key, JSON.stringify(target.item[key] ?? (arrayRules.has(key) ? [] : {}), null, 2)])) }
    case 'switches':
      return { name: target.item.name, management_ip: target.item.management_ip, credential_id: target.item.credential_id, command_profile_id: target.item.command_profile_id }
    case 'networks':
      return { switch_id: target.item.switch_id, cidr: target.item.cidr, vlan: target.item.vlan ?? '', description: target.item.description ?? '', is_validated: target.item.is_validated }
  }
}

export type EditRequest = { path: string; body: Record<string, unknown> } | { invalidRule: string }

// editRequest sends the full editable state with the version it was loaded at; the API ignores
// unchanged values. Empty password fields keep the stored secrets.
export function editRequest(target: EditTarget, form: Form): EditRequest {
  const text = (key: string) => String(form[key] ?? '')
  const path = `${target.kind}/${target.item.id}`
  const version = target.item.version
  switch (target.kind) {
    case 'credentials':
      return {
        path,
        body: {
          version, name: text('name'), username: text('username'), description: text('description'),
          ...(text('password') ? { password: text('password') } : {}),
          ...(form.clear_secret ? { clear_secret: true } : text('secret') ? { secret: text('secret') } : {}),
        },
      }
    case 'profiles': {
      const body: Record<string, unknown> = { version, name: text('name') }
      for (const key of profileRuleKeys) {
        let parsed: unknown
        try {
          parsed = JSON.parse(text(key))
        } catch {
          return { invalidRule: key }
        }
        if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed) !== arrayRules.has(key)) return { invalidRule: key }
        body[key] = parsed
      }
      return { path, body }
    }
    case 'switches':
      return { path, body: { version, name: text('name'), management_ip: text('management_ip').trim(), credential_id: text('credential_id'), command_profile_id: text('command_profile_id') } }
    case 'networks':
      return { path, body: { version, switch_id: text('switch_id'), cidr: text('cidr').trim(), vlan: text('vlan'), description: text('description'), is_validated: Boolean(form.is_validated) } }
  }
}

export function editorLabels(zh: boolean) {
  const noun: Record<AssetKind, string> = zh
    ? { credentials: '凭据', profiles: '命令模板', switches: '交换机', networks: '网段' }
    : { credentials: 'credential', profiles: 'command profile', switches: 'switch', networks: 'network' }
  const switchCount = (count: number) => zh ? `${count} 台交换机` : `${count} switch${count === 1 ? '' : 'es'}`
  return zh ? {
    edit: '编辑', enable: '启用', disable: '停用', disabled: '已停用', unvalidated: '未验证', cancel: '取消', close: '关闭',
    save: '保存修改', saving: '正在保存…', saved: '已保存。',
    name: '名称', username: '设备用户名', description: '描述（可选）', newPassword: '新设备密码', newSecret: '新特权密码',
    keep: '留空则保留现有值', clearSecret: '删除已保存的特权密码', managementIP: '管理 IPv4', credential: '凭据', profile: '命令模板',
    switch: '所属交换机', cidr: 'IPv4 CIDR', vlan: 'VLAN（可选）', validated: '已人工验证网段归属', inactive: '（已停用）',
    editTitle: (kind: AssetKind) => `编辑${noun[kind]}`,
    hint: {
      credentials: '密码不会回显。两个密码框留空即保留现有值。',
      profiles: '保存会改变此模板发送的命令。使用它的交换机上等待确认的查询需要重新执行。',
      switches: '保存后，此交换机上等待确认的查询会失效，操作员需重新查询后才能释放。',
      networks: '只有已启用且已验证的网段参与交换机解析。若修改使某个地址改由其他交换机负责，其等待确认的查询需要重新执行。',
    } satisfies Record<AssetKind, string>,
    invalidRule: (key: string) => `${key} 必须是合法的 JSON ${arrayRules.has(key) ? '数组' : '对象'}。`,
    usedBy: (count: number) => count ? `${switchCount(count)}在用` : '暂无交换机使用',
    ownedBy: (name: string) => `交换机 ${name}`,
    disableTitle: (target: EditTarget) => `停用${noun[target.kind]}「${assetName(target)}」？`,
    disableImpact: (target: EditTarget, inventory?: Inventory, switchName?: string) => {
      const count = switchesUsing(target, inventory)
      switch (target.kind) {
        case 'credentials':
        case 'profiles':
          return count ? `${switchCount(count)}使用此${noun[target.kind]}。重新启用之前，它们不参与解析，排队中的任务会直接失败，不会连接交换机。` : `目前没有交换机使用此${noun[target.kind]}。`
        case 'switches':
          return '其网段内的地址将不再解析到此交换机，等待确认的查询需要重新执行。排队中的任务会直接失败，不会连接交换机。'
        case 'networks':
          return `${target.item.cidr} 内的地址将不再解析到${switchName ? `交换机 ${switchName}` : '该交换机'}。依赖此网段且等待确认的查询需要重新执行。`
      }
    },
    statusChanged: (target: EditTarget, on: boolean) => `${noun[target.kind]}「${assetName(target)}」已${on ? '启用' : '停用'}。`,
    empty: {
      credentials: '还没有凭据。添加交换机之前需要先保存凭据。',
      profiles: '还没有命令模板。添加交换机之前需要先保存模板。',
      switches: '还没有交换机。',
      networks: '还没有网段。只有已验证的网段才会参与解析。',
    } satisfies Record<AssetKind, string>,
  } : {
    edit: 'Edit', enable: 'Enable', disable: 'Disable', disabled: 'Disabled', unvalidated: 'Unvalidated', cancel: 'Cancel', close: 'Close',
    save: 'Save changes', saving: 'Saving…', saved: 'Saved.',
    name: 'Name', username: 'Device username', description: 'Description (optional)', newPassword: 'New device password', newSecret: 'New privilege secret',
    keep: 'Leave empty to keep the stored value', clearSecret: 'Remove the stored privilege secret', managementIP: 'Management IPv4', credential: 'Credential', profile: 'Command profile',
    switch: 'Switch', cidr: 'IPv4 CIDR', vlan: 'VLAN (optional)', validated: 'Network ownership manually validated', inactive: ' (disabled)',
    editTitle: (kind: AssetKind) => `Edit ${noun[kind]}`,
    hint: {
      credentials: 'Stored passwords are never shown. Leave both password fields empty to keep them.',
      profiles: 'Saving changes the commands this profile sends. Pre-queries waiting for confirmation on its switches must be run again.',
      switches: 'Saving invalidates pre-queries waiting for confirmation on this switch. Operators query again before releasing.',
      networks: 'Only enabled, validated networks take part in switch resolution. If this moves an address to another switch, its waiting pre-query must be run again.',
    } satisfies Record<AssetKind, string>,
    invalidRule: (key: string) => `${key} must be a valid JSON ${arrayRules.has(key) ? 'array' : 'object'}.`,
    usedBy: (count: number) => count ? `Used by ${switchCount(count)}` : 'Not used by any switch',
    ownedBy: (name: string) => `Switch ${name}`,
    disableTitle: (target: EditTarget) => `Disable ${noun[target.kind]} “${assetName(target)}”?`,
    disableImpact: (target: EditTarget, inventory?: Inventory, switchName?: string) => {
      const count = switchesUsing(target, inventory)
      switch (target.kind) {
        case 'credentials':
        case 'profiles':
          return count ? `${switchCount(count)} use${count === 1 ? 's' : ''} this ${noun[target.kind]}. Until it is enabled again they stop resolving, and their queued jobs fail without contacting the switch.` : `No switch uses this ${noun[target.kind]}.`
        case 'switches':
          return 'Addresses in its networks stop resolving to it, and pre-queries waiting for confirmation must be run again. Its queued jobs fail without contacting the switch.'
        case 'networks':
          return `Addresses in ${target.item.cidr} stop resolving to ${switchName ? `switch ${switchName}` : 'its switch'}. Waiting pre-queries that relied on it must be run again.`
      }
    },
    statusChanged: (target: EditTarget, on: boolean) => `${noun[target.kind][0].toUpperCase()}${noun[target.kind].slice(1)} “${assetName(target)}” ${on ? 'enabled' : 'disabled'}.`,
    empty: {
      credentials: 'No credentials yet. Switches need one before they can be added.',
      profiles: 'No command profiles yet. Switches need one before they can be added.',
      switches: 'No switches yet.',
      networks: 'No networks yet. Only validated networks take part in resolution.',
    } satisfies Record<AssetKind, string>,
  }
}
export type EditorLabels = ReturnType<typeof editorLabels>

export function AssetRow({ title, detail, on, flag, mono = false, busy, labels, onEdit, onToggle }: {
  title: string; detail?: React.ReactNode; on: boolean; flag?: React.ReactNode; mono?: boolean; busy: boolean
  labels: EditorLabels; onEdit: () => void; onToggle: () => void
}) {
  // The 12rem basis lets the actions wrap below the text on narrow screens instead of squeezing it.
  return <li className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2">
    <div className="min-w-0 flex-[1_1_12rem]">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className={cn('break-words font-medium', mono && 'font-mono', !on && 'text-muted-foreground')}>{title}</span>
        {on ? null : <Badge variant="outline" className="font-normal text-muted-foreground">{labels.disabled}</Badge>}
        {flag}
      </div>
      {detail ? <p className="mt-0.5 break-words text-xs text-muted-foreground">{detail}</p> : null}
    </div>
    <div className="ml-auto flex shrink-0 items-center gap-1">
      <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onEdit} aria-label={`${labels.edit} ${title}`}><Pencil aria-hidden="true" />{labels.edit}</Button>
      <Button type="button" variant="ghost" size="sm" disabled={busy} onClick={onToggle} aria-label={`${on ? labels.disable : labels.enable} ${title}`}>
        {on ? <PowerOff aria-hidden="true" /> : <Power aria-hidden="true" />}{on ? labels.disable : labels.enable}
      </Button>
    </div>
  </li>
}

export function UnvalidatedBadge({ labels }: { labels: EditorLabels }) {
  return <Badge variant="outline" className="border-amber-500/50 font-normal text-amber-800 dark:text-amber-300">{labels.unvalidated}</Badge>
}

function Field({ id, label, children }: { id: string; label: string; children: React.ReactNode }) {
  return <div className="space-y-2"><Label htmlFor={id}>{label}</Label>{children}</div>
}

function EditorForm({ target, inventory, labels, zh, onSave }: {
  target: EditTarget; inventory?: Inventory; labels: EditorLabels; zh: boolean; onSave: (path: string, body: Record<string, unknown>) => Promise<void>
}) {
  const [form, setForm] = useState<Form>(() => initialForm(target))
  const [error, setError] = useState('')
  const [invalidRule, setInvalidRule] = useState('')
  const [busy, setBusy] = useState(false)
  const set = (key: string, value: string | boolean) => setForm((current) => ({ ...current, [key]: value }))
  const text = (id: string, key: string, label: string, options: { required?: boolean; mono?: boolean; inputMode?: 'decimal' } = {}) =>
    <Field id={id} label={label}><Input id={id} value={String(form[key])} onChange={(event) => set(key, event.target.value)} required={options.required} inputMode={options.inputMode} autoComplete="off" className={cn(options.mono && 'font-mono')} /></Field>
  const select = (id: string, key: string, label: string, items: { id: string; name: string; on: boolean }[]) =>
    <Field id={id} label={label}><select id={id} className={selectClass} required value={String(form[key])} onChange={(event) => set(key, event.target.value)}>
      {items.map((item) => <option key={item.id} value={item.id}>{item.name}{item.on ? '' : labels.inactive}</option>)}
    </select></Field>

  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const request = editRequest(target, form)
    if ('invalidRule' in request) {
      setInvalidRule(request.invalidRule)
      setError(labels.invalidRule(request.invalidRule))
      return
    }
    setBusy(true); setError(''); setInvalidRule('')
    try { await onSave(request.path, request.body) }
    catch (cause) { setError(assetErrorText(cause, zh)) }
    finally { setBusy(false) }
  }

  return <form className="space-y-4" onSubmit={(event) => void submit(event)}>
    <DialogHeader>
      <DialogTitle>{labels.editTitle(target.kind)}</DialogTitle>
      <DialogDescription>{labels.hint[target.kind]}</DialogDescription>
    </DialogHeader>
    {target.kind === 'credentials' ? <>
      {text('edit-name', 'name', labels.name, { required: true })}
      {text('edit-username', 'username', labels.username, { required: true })}
      {text('edit-description', 'description', labels.description)}
      <Field id="edit-password" label={labels.newPassword}><Input id="edit-password" type="password" autoComplete="new-password" placeholder={labels.keep} value={String(form.password)} onChange={(event) => set('password', event.target.value)} /></Field>
      <Field id="edit-secret" label={labels.newSecret}><Input id="edit-secret" type="password" autoComplete="new-password" placeholder={labels.keep} disabled={Boolean(form.clear_secret)} value={String(form.secret)} onChange={(event) => set('secret', event.target.value)} /></Field>
      {target.item.has_secret ? <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(form.clear_secret)} onChange={(event) => set('clear_secret', event.target.checked)} />{labels.clearSecret}</label> : null}
    </> : null}
    {target.kind === 'profiles' ? <>
      {text('edit-name', 'name', labels.name, { required: true })}
      {profileRuleKeys.map((key) => <Field key={key} id={`edit-${key}`} label={key}>
        <textarea id={`edit-${key}`} className={cn(ruleClass, invalidRule === key && 'border-destructive')} aria-invalid={invalidRule === key || undefined} spellCheck={false}
          rows={key === 'parser_rules' || key === 'command_templates' ? 6 : 3} value={String(form[key])} onChange={(event) => set(key, event.target.value)} />
      </Field>)}
    </> : null}
    {target.kind === 'switches' ? <>
      {text('edit-name', 'name', labels.name, { required: true })}
      {text('edit-management-ip', 'management_ip', labels.managementIP, { required: true, mono: true, inputMode: 'decimal' })}
      {select('edit-credential', 'credential_id', labels.credential, (inventory?.credentials ?? []).map((item) => ({ id: item.id, name: item.name, on: item.is_active })))}
      {select('edit-profile', 'command_profile_id', labels.profile, (inventory?.profiles ?? []).map((item) => ({ id: item.id, name: item.name, on: item.is_active })))}
    </> : null}
    {target.kind === 'networks' ? <>
      {select('edit-switch', 'switch_id', labels.switch, (inventory?.switches ?? []).map((item) => ({ id: item.id, name: item.name, on: item.is_enabled })))}
      {text('edit-cidr', 'cidr', labels.cidr, { required: true, mono: true })}
      {text('edit-vlan', 'vlan', labels.vlan)}
      {text('edit-description', 'description', labels.description)}
      <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(form.is_validated)} onChange={(event) => set('is_validated', event.target.checked)} />{labels.validated}</label>
    </> : null}
    {error ? <p role="alert" className="rounded-md border border-destructive/40 px-3 py-2 text-sm text-destructive">{error}</p> : null}
    <DialogFooter>
      <DialogClose asChild><Button type="button" variant="outline">{labels.cancel}</Button></DialogClose>
      <Button type="submit" disabled={busy}>{busy ? labels.saving : labels.save}</Button>
    </DialogFooter>
  </form>
}

export function AssetEditDialog({ target, inventory, zh, onClose, onSave }: {
  target: EditTarget | null; inventory?: Inventory; zh: boolean; onClose: () => void; onSave: (path: string, body: Record<string, unknown>) => Promise<void>
}) {
  const labels = editorLabels(zh)
  return <Dialog open={target !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent closeLabel={labels.close} className={cn('max-h-[90dvh] overflow-y-auto', target?.kind === 'profiles' ? 'sm:max-w-2xl' : 'sm:max-w-lg')}>
      {target ? <EditorForm key={`${target.kind}:${target.item.id}:${target.item.version}`} target={target} inventory={inventory} labels={labels} zh={zh} onSave={onSave} /> : null}
    </DialogContent>
  </Dialog>
}

export function DisableAssetDialog({ target, inventory, zh, onCancel, onConfirm }: {
  target: EditTarget | null; inventory?: Inventory; zh: boolean; onCancel: () => void; onConfirm: (target: EditTarget) => void
}) {
  const labels = editorLabels(zh)
  const switchName = target?.kind === 'networks' ? inventory?.switches.find((item) => item.id === target.item.switch_id)?.name : undefined
  return <AlertDialog open={target !== null} onOpenChange={(open) => { if (!open) onCancel() }}>
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{target ? labels.disableTitle(target) : ''}</AlertDialogTitle>
        <AlertDialogDescription>{target ? labels.disableImpact(target, inventory, switchName) : ''}</AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>{labels.cancel}</AlertDialogCancel>
        <AlertDialogAction onClick={() => { if (target) onConfirm(target) }}>{labels.disable}</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
}

const fieldNames: Record<string, [string, string]> = {
  name: ['name', '名称'], username: ['username', '用户名'], description: ['description', '描述'],
  encrypted_password: ['password', '密码'], encrypted_secret: ['privilege secret', '特权密码'],
  management_ip: ['management IP', '管理 IP'], credential_id: ['credential', '凭据'], command_profile_id: ['command profile', '命令模板'],
  switch_id: ['switch', '所属交换机'], cidr: ['CIDR', '网段'], vlan: ['VLAN', 'VLAN'], is_validated: ['validation', '验证状态'],
}

// describeChanges summarizes an audit record without repeating any stored value.
export function describeChanges(changes: Record<string, unknown> | undefined, zh: boolean): string {
  const parts = Object.entries(changes ?? {}).map(([key, value]) => {
    if (key === 'is_active' || key === 'is_enabled') {
      const to = (value as { to?: unknown }).to
      return zh ? (to ? '启用' : '停用') : (to ? 'enabled' : 'disabled')
    }
    const name = fieldNames[key]?.[zh ? 1 : 0] ?? key
    if (value === 'replaced') return zh ? `${name}已更换` : `${name} replaced`
    if (value === 'cleared') return zh ? `${name}已删除` : `${name} removed`
    return name
  })
  return parts.length ? parts.join(zh ? '、' : ', ') : '—'
}

const actionNames: Record<string, [string, string]> = {
  'credential.create': ['Credential created', '新建凭据'], 'credential.update': ['Credential changed', '修改凭据'],
  'profile.create': ['Profile created', '新建命令模板'], 'profile.update': ['Profile changed', '修改命令模板'],
  'switch.create': ['Switch created', '新建交换机'], 'switch.update': ['Switch changed', '修改交换机'],
  'network.create': ['Network created', '新建网段'], 'network.update': ['Network changed', '修改网段'],
}

export function actionName(action: string, zh: boolean): string {
  return actionNames[action]?.[zh ? 1 : 0] ?? action
}
