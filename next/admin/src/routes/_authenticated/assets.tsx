import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  AssetEditDialog, AssetRequestError, AssetRow, DisableAssetDialog, UnvalidatedBadge, actionName, assetErrorText, callAssets,
  describeChanges, editorLabels, isOn, selectClass, statusBody, switchesUsing, type AssetKind, type EditTarget, type Inventory,
} from '@/features/release/asset-editors'

export const Route = createFileRoute('/_authenticated/assets')({ component: Assets })
type AssetEvent = { id: number; actor_email: string; action: string; object_name: string; changes?: Record<string, unknown>; created_at: string }

function Assets() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { i18n } = useTranslation()
  const zh = i18n.language.startsWith('zh')
  const admin = Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin'))
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin })
  const audit = useQuery({ queryKey: ['release-asset-audit', user.id], queryFn: () => callAssets<AssetEvent[]>('audit'), enabled: admin })
  const [credential, setCredential] = useState({ name: '', username: '', password: '', secret: '' })
  const [profile, setProfile] = useState({ name: '', command_templates: '{"single_arp_query":"","arp_release":""}', parser_rules: '{"arp_entry_regex":"","missing_patterns":[],"static_type_values":[],"dynamic_type_values":[]}', login_prompt_patterns: '{}', prompt_patterns: '{}', pagination_rules: '{}', success_patterns: '[]', error_patterns: '[]' })
  const [asset, setAsset] = useState({ name: '', management_ip: '', credential_id: '', command_profile_id: '' })
  const [network, setNetwork] = useState({ switch_id: '', cidr: '', is_validated: false })
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [disabling, setDisabling] = useState<EditTarget | null>(null)
  const labels = editorLabels(zh)
  const data = inventory.data
  const credentialNames = new Map(data?.credentials.map((item) => [item.id, item.name] as const))
  const profileNames = new Map(data?.profiles.map((item) => [item.id, item.name] as const))
  const switchNames = new Map(data?.switches.map((item) => [item.id, item.name] as const))

  async function refresh() { await Promise.all([inventory.refetch(), audit.refetch()]) }
  async function submit(path: string, body: unknown) {
    setBusy(true); setMessage('')
    try { await callAssets(path, body); if (path === 'credentials') setCredential((current) => ({ ...current, password: '', secret: '' })); await refresh(); setMessage(labels.saved) }
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
  const field = (id: string, label: string, value: string, onChange: (value: string) => void, type = 'text') => <div className="space-y-2" key={id}><Label htmlFor={id}>{label}</Label><Input id={id} type={type} value={value} onChange={(event) => onChange(event.target.value)} required={type !== 'password' || id === 'credential-password'} autoComplete="off" /></div>
  return <main className="mx-auto flex w-full max-w-5xl flex-col gap-6">
    <header className="space-y-2"><h1 className="text-2xl font-semibold">{zh ? '交换机资产配置' : 'Switch assets'}</h1><p className="text-sm text-muted-foreground">{zh ? '先保存凭据和命令模板，再添加交换机与已验证的网段。请只连接你授权管理的设备。' : 'Save a credential and command profile, then add a switch and validated network. Only connect authorized devices.'}</p></header>
    {message ? <p role="status" className="rounded-md border px-3 py-2 text-sm">{message}</p> : null}
    <div className="grid gap-6 lg:grid-cols-2">
      <form className="space-y-4 rounded-xl border p-5" onSubmit={(event) => { event.preventDefault(); void submit('credentials', credential) }}><h2 className="font-semibold">{zh ? '凭据' : 'Credential'}</h2>
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
      <form className="space-y-4 rounded-xl border p-5" onSubmit={(event) => { event.preventDefault(); try { void submit('profiles', Object.fromEntries(Object.entries(profile).map(([k, v]) => [k, k === 'name' ? v : JSON.parse(v) as unknown]))) } catch { setMessage(zh ? '模板 JSON 无效' : 'Invalid profile JSON') } }}><h2 className="font-semibold">{zh ? '命令模板' : 'Command profile'}</h2>
        {field('profile-name', zh ? '名称' : 'Name', profile.name, (v) => setProfile({ ...profile, name: v }))}
        {(['command_templates', 'parser_rules', 'login_prompt_patterns', 'prompt_patterns', 'pagination_rules', 'success_patterns', 'error_patterns'] as const).map((key) => <div className="space-y-2" key={key}><Label htmlFor={key}>{key}</Label><textarea id={key} className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs" rows={key === 'parser_rules' ? 4 : 2} value={profile[key]} onChange={(event) => setProfile({ ...profile, [key]: event.target.value })} /></div>)}
        <Button disabled={busy}>{zh ? '保存模板' : 'Save profile'}</Button>
        {rows('profiles', (data?.profiles ?? []).map((item) => {
          const target: EditTarget = { kind: 'profiles', item }
          return <AssetRow key={item.id} title={item.name} on={item.is_active} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
            detail={labels.usedBy(switchesUsing(target, data))} />
        }))}
      </form>
      <form className="space-y-4 rounded-xl border p-5" onSubmit={(event) => { event.preventDefault(); void submit('switches', asset) }}><h2 className="font-semibold">{zh ? '交换机' : 'Switch'}</h2>
        {field('switch-name', zh ? '名称' : 'Name', asset.name, (v) => setAsset({ ...asset, name: v }))}
        {field('switch-ip', zh ? '管理 IPv4' : 'Management IPv4', asset.management_ip, (v) => setAsset({ ...asset, management_ip: v }))}
        {(['credential_id', 'command_profile_id'] as const).map((key) => {
          const options: { id: string; name: string }[] = (key === 'credential_id' ? data?.credentials : data?.profiles) ?? []
          return <div className="space-y-2" key={key}><Label htmlFor={key}>{key === 'credential_id' ? (zh ? '凭据' : 'Credential') : (zh ? '命令模板' : 'Command profile')}</Label><select id={key} className={selectClass} required value={asset[key]} onChange={(event) => setAsset({ ...asset, [key]: event.target.value })}><option value="">—</option>{options.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></div>
        })}
        <Button disabled={busy}>{zh ? '保存交换机' : 'Save switch'}</Button>
        {rows('switches', (data?.switches ?? []).map((item) => {
          const target: EditTarget = { kind: 'switches', item }
          return <AssetRow key={item.id} title={item.name} on={item.is_enabled} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
            detail={<><span className="font-mono">{item.management_ip}</span> · {credentialNames.get(item.credential_id) ?? '—'} · {profileNames.get(item.command_profile_id) ?? '—'}</>} />
        }))}
      </form>
      <form className="space-y-4 rounded-xl border p-5" onSubmit={(event) => { event.preventDefault(); void submit('networks', network) }}><h2 className="font-semibold">{zh ? '网段' : 'Network'}</h2>
        <div className="space-y-2"><Label htmlFor="network-switch">{zh ? '所属交换机' : 'Switch'}</Label><select id="network-switch" className={selectClass} required value={network.switch_id} onChange={(event) => setNetwork({ ...network, switch_id: event.target.value })}><option value="">—</option>{data?.switches.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></div>
        {field('network-cidr', 'IPv4 CIDR', network.cidr, (v) => setNetwork({ ...network, cidr: v }))}
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={network.is_validated} onChange={(event) => setNetwork({ ...network, is_validated: event.target.checked })} />{zh ? '已人工验证网段归属' : 'Network ownership manually validated'}</label>
        <Button disabled={busy}>{zh ? '保存网段' : 'Save network'}</Button>
        {rows('networks', (data?.networks ?? []).map((item) => {
          const target: EditTarget = { kind: 'networks', item }
          return <AssetRow key={item.id} title={item.cidr} mono on={item.is_enabled} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
            flag={item.is_validated ? null : <UnvalidatedBadge labels={labels} />}
            detail={[labels.ownedBy(switchNames.get(item.switch_id) ?? '—'), item.vlan ? `VLAN ${item.vlan}` : '', item.description].filter(Boolean).join(' · ')} />
        }))}
      </form>
    </div>
    <section className="space-y-3"><h2 className="text-lg font-medium">{zh ? '资产操作记录' : 'Asset changes'}</h2><div className="overflow-x-auto rounded-xl border"><table className="w-full min-w-[720px] text-left text-sm"><thead className="bg-muted/50"><tr><th className="p-3">{zh ? '时间' : 'Time'}</th><th className="p-3">{zh ? '操作者' : 'Actor'}</th><th className="p-3">{zh ? '操作' : 'Action'}</th><th className="p-3">{zh ? '对象' : 'Object'}</th><th className="p-3">{zh ? '变更' : 'Changes'}</th></tr></thead><tbody>{audit.data?.map((event) => <tr key={event.id} className="border-t"><td className="p-3 tabular-nums">{new Date(event.created_at).toLocaleString()}</td><td className="p-3">{event.actor_email}</td><td className="p-3">{actionName(event.action, zh)}</td><td className="p-3">{event.object_name}</td><td className="p-3 text-muted-foreground">{describeChanges(event.changes, zh)}</td></tr>)}</tbody></table>{!audit.data?.length ? <p className="p-3 text-sm text-muted-foreground">{zh ? '暂无资产变更' : 'No asset changes'}</p> : null}</div></section>
    <AssetEditDialog target={editing} inventory={data} zh={zh} onClose={() => setEditing(null)} onSave={saveEdit} />
    <DisableAssetDialog target={disabling} inventory={data} zh={zh} onCancel={() => setDisabling(null)} onConfirm={(target) => void setStatus(target, false)} />
  </main>
}
