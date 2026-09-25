import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export const Route = createFileRoute('/_authenticated/assets')({ component: Assets })
type Item = { id: string; name?: string; cidr?: string; management_ip?: string; is_validated?: boolean }
type Inventory = { credentials: Item[]; profiles: Item[]; switches: Item[]; networks: Item[] }
type AssetEvent = { id: number; actor_email: string; action: string; object_name: string; created_at: string }

async function call<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(`/api/releases/admin/${path}`, {
    method: body ? 'POST' : 'GET', credentials: 'same-origin', cache: 'no-store',
    headers: { Accept: 'application/json', ...(body ? { 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  })
  const data: unknown = await response.json()
  if (!response.ok) throw new Error((data as { detail?: string }).detail || `HTTP ${response.status}`)
  return data as T
}

function Assets() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { i18n } = useTranslation()
  const zh = i18n.language.startsWith('zh')
  const admin = Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin'))
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => call<Inventory>('assets'), enabled: admin })
  const audit = useQuery({ queryKey: ['release-asset-audit', user.id], queryFn: () => call<AssetEvent[]>('audit'), enabled: admin })
  const [credential, setCredential] = useState({ name: '', username: '', password: '', secret: '' })
  const [profile, setProfile] = useState({ name: '', command_templates: '{"single_arp_query":"","arp_release":""}', parser_rules: '{"arp_entry_regex":"","missing_patterns":[],"static_type_values":[],"dynamic_type_values":[]}', login_prompt_patterns: '{}', prompt_patterns: '{}', pagination_rules: '{}', success_patterns: '[]', error_patterns: '[]' })
  const [asset, setAsset] = useState({ name: '', management_ip: '', credential_id: '', command_profile_id: '' })
  const [network, setNetwork] = useState({ switch_id: '', cidr: '', is_validated: false })
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(path: string, data: unknown) {
    setBusy(true); setMessage('')
    try { await call(path, data); if (path === 'credentials') setCredential((current) => ({ ...current, password: '', secret: '' })); await Promise.all([inventory.refetch(), audit.refetch()]); setMessage(zh ? '已保存。' : 'Saved.') }
    catch (error) { setMessage(error instanceof Error ? error.message : 'Request failed') }
    finally { setBusy(false) }
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
        <ul className="space-y-1 text-sm text-muted-foreground">{inventory.data?.credentials.map((item) => <li key={item.id}>{item.name} · <span className="font-mono text-xs">{item.id}</span></li>)}</ul>
      </form>
      <form className="space-y-4 rounded-xl border p-5" onSubmit={(event) => { event.preventDefault(); try { void submit('profiles', Object.fromEntries(Object.entries(profile).map(([k, v]) => [k, k === 'name' ? v : JSON.parse(v) as unknown]))) } catch { setMessage(zh ? '模板 JSON 无效' : 'Invalid profile JSON') } }}><h2 className="font-semibold">{zh ? '命令模板' : 'Command profile'}</h2>
        {field('profile-name', zh ? '名称' : 'Name', profile.name, (v) => setProfile({ ...profile, name: v }))}
        {(['command_templates', 'parser_rules', 'login_prompt_patterns', 'prompt_patterns', 'pagination_rules', 'success_patterns', 'error_patterns'] as const).map((key) => <div className="space-y-2" key={key}><Label htmlFor={key}>{key}</Label><textarea id={key} className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs" rows={key === 'parser_rules' ? 4 : 2} value={profile[key]} onChange={(event) => setProfile({ ...profile, [key]: event.target.value })} /></div>)}
        <Button disabled={busy}>{zh ? '保存模板' : 'Save profile'}</Button><ul className="space-y-1 text-sm text-muted-foreground">{inventory.data?.profiles.map((item) => <li key={item.id}>{item.name} · <span className="font-mono text-xs">{item.id}</span></li>)}</ul>
      </form>
      <form className="space-y-4 rounded-xl border p-5" onSubmit={(event) => { event.preventDefault(); void submit('switches', asset) }}><h2 className="font-semibold">{zh ? '交换机' : 'Switch'}</h2>
        {field('switch-name', zh ? '名称' : 'Name', asset.name, (v) => setAsset({ ...asset, name: v }))}
        {field('switch-ip', zh ? '管理 IPv4' : 'Management IPv4', asset.management_ip, (v) => setAsset({ ...asset, management_ip: v }))}
        {(['credential_id', 'command_profile_id'] as const).map((key) => <div className="space-y-2" key={key}><Label htmlFor={key}>{key}</Label><select id={key} className="w-full rounded-md border bg-background px-3 py-2 text-sm" required value={asset[key]} onChange={(event) => setAsset({ ...asset, [key]: event.target.value })}><option value="">—</option>{(key === 'credential_id' ? inventory.data?.credentials : inventory.data?.profiles)?.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></div>)}
        <Button disabled={busy}>{zh ? '保存交换机' : 'Save switch'}</Button><ul className="space-y-1 text-sm text-muted-foreground">{inventory.data?.switches.map((item) => <li key={item.id}>{item.name} · {item.management_ip}</li>)}</ul>
      </form>
      <form className="space-y-4 rounded-xl border p-5" onSubmit={(event) => { event.preventDefault(); void submit('networks', network) }}><h2 className="font-semibold">{zh ? '网段' : 'Network'}</h2>
        <div className="space-y-2"><Label htmlFor="network-switch">{zh ? '所属交换机' : 'Switch'}</Label><select id="network-switch" className="w-full rounded-md border bg-background px-3 py-2 text-sm" required value={network.switch_id} onChange={(event) => setNetwork({ ...network, switch_id: event.target.value })}><option value="">—</option>{inventory.data?.switches.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></div>
        {field('network-cidr', 'IPv4 CIDR', network.cidr, (v) => setNetwork({ ...network, cidr: v }))}
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={network.is_validated} onChange={(event) => setNetwork({ ...network, is_validated: event.target.checked })} />{zh ? '已人工验证网段归属' : 'Network ownership manually validated'}</label>
        <Button disabled={busy}>{zh ? '保存网段' : 'Save network'}</Button><ul className="space-y-1 text-sm text-muted-foreground">{inventory.data?.networks.map((item) => <li key={item.id} className="font-mono">{item.cidr} · {item.is_validated ? '✓' : '—'}</li>)}</ul>
      </form>
    </div>
    <section className="space-y-3"><h2 className="text-lg font-medium">{zh ? '资产操作记录' : 'Asset changes'}</h2><div className="overflow-x-auto rounded-xl border"><table className="w-full min-w-[580px] text-left text-sm"><thead className="bg-muted/50"><tr><th className="p-3">{zh ? '时间' : 'Time'}</th><th className="p-3">{zh ? '操作者' : 'Actor'}</th><th className="p-3">{zh ? '操作' : 'Action'}</th><th className="p-3">{zh ? '对象' : 'Object'}</th></tr></thead><tbody>{audit.data?.map((event) => <tr key={event.id} className="border-t"><td className="p-3">{new Date(event.created_at).toLocaleString()}</td><td className="p-3">{event.actor_email}</td><td className="p-3">{event.action}</td><td className="p-3">{event.object_name}</td></tr>)}</tbody></table>{!audit.data?.length ? <p className="p-3 text-sm text-muted-foreground">{zh ? '暂无资产变更' : 'No asset changes'}</p> : null}</div></section>
  </main>
}
