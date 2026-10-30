import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  AssetEditDialog, AssetRequestError, AssetRow, DisableAssetDialog, UnvalidatedBadge, assetErrorText, callAssets,
  editorLabels, isOn, selectClass, statusBody, switchesUsing, type AssetKind, type EditTarget, type Inventory,
} from '@/features/release/asset-editors'
import { useRequiredFields } from '@/features/release/required-fields'

export const Route = createFileRoute('/_authenticated/assets')({ component: Assets })

function Assets() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { i18n } = useTranslation()
  const zh = i18n.language.startsWith('zh')
  const admin = Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin'))
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin })
  const [credential, setCredential] = useState({ name: '', username: '', password: '', secret: '' })
  const [profile, setProfile] = useState({ name: '', command_templates: '{"single_arp_query":"","arp_release":"","config_backup":"display current-configuration"}', parser_rules: '{"arp_entry_regex":"","missing_patterns":[],"static_type_values":[],"dynamic_type_values":[]}', login_prompt_patterns: '{}', prompt_patterns: '{}', pagination_rules: '{}', success_patterns: '[]', error_patterns: '[]' })
  const [group, setGroup] = useState({ name: '', description: '' })
  const [network, setNetwork] = useState({ switch_id: '', cidr: '', is_validated: false })
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [disabling, setDisabling] = useState<EditTarget | null>(null)
  const required = useRequiredFields(zh)
  const labels = editorLabels(zh)
  const data = inventory.data
  const switchNames = new Map(data?.switches.map((item) => [item.id, item.name] as const))

  async function refresh() { await inventory.refetch() }
  async function submit(path: string, body: unknown, reset?: () => void) {
    setBusy(true); setMessage('')
    try { await callAssets(path, body); reset?.(); await refresh(); setMessage(labels.saved) }
    catch (error) { setMessage(assetErrorText(error, zh)) }
    finally { setBusy(false) }
  }
  // Errors stay in the open editor; a stale version also reloads the list behind it.
  async function saveEdit(path: string, body: Record<string, unknown>) {
    try { await callAssets(path, body, 'PATCH') }
    catch (error) { if (error instanceof AssetRequestError && error.status === 409) await refresh(); throw error }
    setEditing(null); setMessage(labels.saved); await refresh()
  }
  async function setStatus(target: EditTarget, on: boolean) {
    setBusy(true); setMessage('')
    try { await callAssets(`${target.kind}/${target.item.id}`, statusBody(target, on), 'PATCH'); setMessage(labels.statusChanged(target, on)) }
    catch (error) { setMessage(assetErrorText(error, zh)) }
    finally { await refresh(); setBusy(false) }
  }
  // Enabling is immediate; disabling explains its effect on resolution and queued jobs first.
  function toggle(target: EditTarget) { if (isOn(target)) setDisabling(target); else void setStatus(target, true) }
  function rows(kind: AssetKind, items: React.ReactNode[]) {
    return <div className="border-t pt-2">{items.length ? <ul className="divide-y">{items}</ul> : <p className="py-2 text-sm text-muted-foreground">{labels.empty[kind]}</p>}</div>
  }

  if (!admin) return <p role="alert">{zh ? '仅管理员可管理交换机资产。' : 'Admin role required.'}</p>
  const field = (id: string, label: string, value: string, onChange: (value: string) => void, type = 'text') => <div className="space-y-2" key={id}><Label htmlFor={id}>{label}</Label><Input id={id} type={type} value={value} onChange={(event) => onChange(event.target.value)} required={type !== 'password' || id === 'credential-password'} {...required.invalid(id)} autoComplete="off" />{required.message(id)}</div>
  // Every form here checks its own required fields first; `save` runs only when they are filled.
  const onSubmit = (save: () => void) => (event: React.FormEvent<HTMLFormElement>) => { event.preventDefault(); if (required.check(event.currentTarget)) save() }
  return <main className="mx-auto flex w-full max-w-5xl flex-col gap-6">
    <header className="space-y-2"><h1 className="text-2xl font-semibold">{zh ? '资产配置' : 'Asset settings'}</h1><p className="text-sm text-muted-foreground">{zh ? '凭据、命令模板、分组和网段。交换机本身在' : 'Credentials, command profiles, groups and networks. Switches themselves are on the '}<Link to="/devices" className="underline underline-offset-4">{zh ? '设备' : 'Devices'}</Link>{zh ? '页面管理；所有变更记录见' : ' page; every change is in the '}<Link to="/audit" className="underline underline-offset-4">{zh ? '审计日志' : 'audit log'}</Link>{zh ? '。' : '.'}</p></header>
    {message ? <p role="status" className="rounded-md border px-3 py-2 text-sm">{message}</p> : null}
    <div className="grid gap-6 lg:grid-cols-2">
      <form className="space-y-4 rounded-xl border p-5" noValidate onChange={required.onChange} onSubmit={onSubmit(() => void submit('credentials', credential, () => setCredential({ ...credential, password: '', secret: '' })))}><h2 className="font-semibold">{zh ? '凭据' : 'Credential'}</h2>
        {field('credential-name', zh ? '名称' : 'Name', credential.name, (v) => setCredential({ ...credential, name: v }))}
        {field('credential-username', zh ? '设备用户名' : 'Device username', credential.username, (v) => setCredential({ ...credential, username: v }))}
        {field('credential-password', zh ? '设备密码' : 'Device password', credential.password, (v) => setCredential({ ...credential, password: v }), 'password')}
        <div className="space-y-2"><Label htmlFor="credential-secret">{zh ? '特权密码（可选）' : 'Privilege secret (optional)'}</Label><Input id="credential-secret" type="password" autoComplete="off" value={credential.secret} onChange={(event) => setCredential({ ...credential, secret: event.target.value })} /></div>
        <Button disabled={busy}>{zh ? '保存凭据' : 'Save credential'}</Button>
        {rows('credentials', (data?.credentials ?? []).map((item) => {
          const target: EditTarget = { kind: 'credentials', item }
          return <AssetRow key={item.id} title={item.name} on={item.is_active} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
            detail={[item.username, item.description, labels.usedBy(switchesUsing(target, data))].filter(Boolean).join(' · ')} />
        }))}
      </form>
      <form className="space-y-4 rounded-xl border p-5" noValidate onChange={required.onChange} onSubmit={onSubmit(() => { try { void submit('profiles', Object.fromEntries(Object.entries(profile).map(([k, v]) => [k, k === 'name' ? v : JSON.parse(v) as unknown]))) } catch { setMessage(zh ? '模板 JSON 无效' : 'Invalid profile JSON') } })}><h2 className="font-semibold">{zh ? '命令模板' : 'Command profile'}</h2>
        {field('profile-name', zh ? '名称' : 'Name', profile.name, (v) => setProfile({ ...profile, name: v }))}
        {(['command_templates', 'parser_rules', 'login_prompt_patterns', 'prompt_patterns', 'pagination_rules', 'success_patterns', 'error_patterns'] as const).map((key) => <div className="space-y-2" key={key}><Label htmlFor={key}>{key}</Label><textarea id={key} className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs" rows={key === 'parser_rules' || key === 'command_templates' ? 4 : 2} value={profile[key]} onChange={(event) => setProfile({ ...profile, [key]: event.target.value })} /></div>)}
        <p className="text-xs text-muted-foreground">{zh ? 'command_templates 中的 config_backup 是配置备份命令，例如华为的 display current-configuration。' : 'config_backup in command_templates is the backup command, such as display current-configuration on Huawei.'}</p>
        <Button disabled={busy}>{zh ? '保存模板' : 'Save profile'}</Button>
        {rows('profiles', (data?.profiles ?? []).map((item) => {
          const target: EditTarget = { kind: 'profiles', item }
          return <AssetRow key={item.id} title={item.name} on={item.is_active} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
            detail={labels.usedBy(switchesUsing(target, data))} />
        }))}
      </form>
      <form className="space-y-4 rounded-xl border p-5" noValidate onChange={required.onChange} onSubmit={onSubmit(() => void submit('groups', group, () => setGroup({ name: '', description: '' })))}><h2 className="font-semibold">{zh ? '分组' : 'Group'}</h2>
        {field('group-name', zh ? '名称' : 'Name', group.name, (v) => setGroup({ ...group, name: v }))}
        <div className="space-y-2"><Label htmlFor="group-description">{zh ? '描述（可选）' : 'Description (optional)'}</Label><Input id="group-description" value={group.description} onChange={(event) => setGroup({ ...group, description: event.target.value })} /></div>
        <Button disabled={busy}>{zh ? '保存分组' : 'Save group'}</Button>
        {rows('groups', (data?.groups ?? []).map((item) => {
          const count = data?.switches.filter((s) => s.group_id === item.id).length ?? 0
          return <AssetRow key={item.id} title={item.name} on busy={busy} labels={labels} onEdit={() => setEditing({ kind: 'groups', item })}
            detail={[item.description, zh ? `${count} 台交换机` : `${count} switch${count === 1 ? '' : 'es'}`].filter(Boolean).join(' · ')} />
        }))}
      </form>
      <form className="space-y-4 rounded-xl border p-5" noValidate onChange={required.onChange} onSubmit={onSubmit(() => void submit('networks', network))}><h2 className="font-semibold">{zh ? '网段' : 'Network'}</h2>
        <div className="space-y-2"><Label htmlFor="network-switch">{zh ? '所属交换机' : 'Switch'}</Label><select id="network-switch" className={selectClass} required {...required.invalid('network-switch')} value={network.switch_id} onChange={(event) => setNetwork({ ...network, switch_id: event.target.value })}><option value="">—</option>{data?.switches.map((item) => <option key={item.id} value={item.id}>{item.name} ({item.management_ip})</option>)}</select>{required.message('network-switch')}</div>
        {field('network-cidr', 'IPv4 CIDR', network.cidr, (v) => setNetwork({ ...network, cidr: v }))}
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={network.is_validated} onChange={(event) => setNetwork({ ...network, is_validated: event.target.checked })} />{zh ? '已人工验证网段归属' : 'Network ownership manually validated'}</label>
        <Button disabled={busy}>{zh ? '保存网段' : 'Save network'}</Button>
        <div className="max-h-[32rem] overflow-y-auto">{rows('networks', (data?.networks ?? []).map((item) => {
          const target: EditTarget = { kind: 'networks', item }
          return <AssetRow key={item.id} title={item.cidr} mono on={item.is_enabled} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
            flag={item.is_validated ? null : <UnvalidatedBadge labels={labels} />}
            detail={[labels.ownedBy(switchNames.get(item.switch_id) ?? '—'), item.vlan ? `VLAN ${item.vlan}` : '', item.description].filter(Boolean).join(' · ')} />
        }))}</div>
      </form>
    </div>
    <AssetEditDialog target={editing} inventory={data} zh={zh} onClose={() => setEditing(null)} onSave={saveEdit} />
    <DisableAssetDialog target={disabling} inventory={data} zh={zh} onCancel={() => setDisabling(null)} onConfirm={(target) => void setStatus(target, false)} />
  </main>
}
