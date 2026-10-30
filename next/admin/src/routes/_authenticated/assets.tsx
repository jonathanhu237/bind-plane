import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Lock } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { Empty, PageHeader } from '@/features/ops/ui'
import {
  AssetEditDialog, AssetRequestError, AssetRow, DisableAssetDialog, UnvalidatedBadge, callAssets, editorLabels, isOn,
  profileRuleKeys, ruleHint, ruleLabel, statusBody, switchesUsing, type AssetKind, type EditTarget, type Inventory,
} from '@/features/release/asset-editors'
import { errorText } from '@/features/release/messages'
import { useRequiredFields } from '@/features/release/required-fields'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

export const Route = createFileRoute('/_authenticated/assets')({ component: Assets })

const emptyProfile = {
  name: '', command_templates: '{"single_arp_query":"","arp_release":"","config_backup":"display current-configuration"}',
  parser_rules: '{"arp_entry_regex":"","missing_patterns":[],"static_type_values":[],"dynamic_type_values":[]}',
  login_prompt_patterns: '{}', prompt_patterns: '{}', pagination_rules: '{}', success_patterns: '[]', error_patterns: '[]',
}

function Assets() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { i18n, t } = useTranslation()
  const zh = i18n.language.startsWith('zh')
  const admin = Boolean(user.superAdmin || user.roles?.some((role) => role.name === 'Admin'))
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin })
  const [credential, setCredential] = useState({ name: '', username: '', password: '', secret: '' })
  const [profile, setProfile] = useState(emptyProfile)
  const [group, setGroup] = useState({ name: '', description: '' })
  const [network, setNetwork] = useState({ switch_id: '', cidr: '', is_validated: false })
  const [errors, setErrors] = useState<Partial<Record<AssetKind, string>>>({})
  const [busy, setBusy] = useState(false)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [disabling, setDisabling] = useState<EditTarget | null>(null)
  const labels = editorLabels(zh)
  const name = zh ? '名称' : 'Name'
  const credentialRequired = useRequiredFields(zh, [
    { id: 'credential-name', label: name, value: credential.name },
    { id: 'credential-username', label: zh ? '设备用户名' : 'Device username', value: credential.username },
    { id: 'credential-password', label: zh ? '设备密码' : 'Device password', value: credential.password },
  ])
  const profileRequired = useRequiredFields(zh, [{ id: 'profile-name', label: name, value: profile.name }])
  const groupRequired = useRequiredFields(zh, [{ id: 'group-name', label: name, value: group.name }])
  const networkRequired = useRequiredFields(zh, [
    { id: 'network-switch', label: zh ? '所属交换机' : 'Switch', value: network.switch_id, choose: true },
    { id: 'network-cidr', label: 'IPv4 CIDR', value: network.cidr },
  ])
  const data = inventory.data
  const switchNames = new Map(data?.switches.map((item) => [item.id, item.name] as const))

  async function refresh() { await inventory.refetch() }
  async function submit(kind: AssetKind, body: unknown, reset: () => void) {
    setBusy(true); setErrors((current) => ({ ...current, [kind]: undefined }))
    try { await callAssets(kind, body); reset(); notifySuccess(labels.saved); await refresh() }
    catch (error) { setErrors((current) => ({ ...current, [kind]: errorText(error, zh) })) }
    finally { setBusy(false) }
  }
  // Errors stay in the open editor; a stale version also reloads the list behind it.
  async function saveEdit(path: string, body: Record<string, unknown>) {
    try { await callAssets(path, body, 'PATCH') }
    catch (error) { if (error instanceof AssetRequestError && error.status === 409) await refresh(); throw error }
    setEditing(null); notifySuccess(labels.saved); await refresh()
  }
  async function setStatus(target: EditTarget, on: boolean) {
    setBusy(true)
    try { await callAssets(`${target.kind}/${target.item.id}`, statusBody(target, on), 'PATCH'); notifySuccess(labels.statusChanged(target, on)) }
    catch (error) { notifyRequestError(error, t, { description: errorText(error, zh) }) }
    finally { await refresh(); setBusy(false) }
  }
  // Enabling is immediate; disabling explains its effect on resolution and queued jobs first.
  function toggle(target: EditTarget) { if (isOn(target)) setDisabling(target); else void setStatus(target, true) }
  function rows(kind: AssetKind, items: React.ReactNode[]) {
    return items.length ? <ul className="divide-y">{items}</ul> : <Empty>{labels.empty[kind]}</Empty>
  }
  function failure(kind: AssetKind) {
    return errors[kind] ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{errors[kind]}</AlertDescription></Alert> : null
  }

  if (!admin) return <Alert><Lock aria-hidden="true" /><AlertDescription>{zh ? '仅管理员可以管理交换机资产。' : 'Managing switch assets needs the Admin role.'}</AlertDescription></Alert>
  const text = (required: ReturnType<typeof useRequiredFields>, id: string, label: string, value: string, onChange: (value: string) => void, type = 'text') =>
    <Field data-invalid={required.invalid(id)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input id={id} type={type} value={value} onChange={(event) => onChange(event.target.value)} {...required.control(id)} autoComplete="off" />
      {required.message(id)}
    </Field>
  const onSubmit = (required: ReturnType<typeof useRequiredFields>, save: () => void) => (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (required.check()) save()
  }
  return <main className="mx-auto flex w-full max-w-5xl flex-col gap-6">
    <PageHeader title={zh ? '资产配置' : 'Asset settings'} description={<>
      {zh ? '凭据、命令模板、分组和网段。交换机本身在' : 'Credentials, command profiles, groups and networks. Switches themselves are on the '}
      <Link to="/devices" className="underline underline-offset-4">{zh ? '设备' : 'Devices'}</Link>
      {zh ? '页面管理；所有变更记录见' : ' page; every change is in the '}
      <Link to="/audit" className="underline underline-offset-4">{zh ? '审计日志' : 'audit log'}</Link>{zh ? '。' : '.'}
    </>} />
    <div className="grid gap-6 lg:grid-cols-2">
      <Card>
        <CardHeader><CardTitle>{zh ? '凭据' : 'Credentials'}</CardTitle><CardDescription>{zh ? '登录交换机所用的账号，加密保存，不会回显。' : 'Accounts for signing in to switches, stored encrypted and never shown again.'}</CardDescription></CardHeader>
        <CardContent className="flex flex-col gap-6">
          <form noValidate className="flex flex-col gap-6" onSubmit={onSubmit(credentialRequired, () => void submit('credentials', credential, () => { setCredential({ name: '', username: '', password: '', secret: '' }); credentialRequired.reset() }))}>
            <FieldGroup>
              {text(credentialRequired, 'credential-name', name, credential.name, (v) => setCredential({ ...credential, name: v }))}
              {text(credentialRequired, 'credential-username', zh ? '设备用户名' : 'Device username', credential.username, (v) => setCredential({ ...credential, username: v }))}
              {text(credentialRequired, 'credential-password', zh ? '设备密码' : 'Device password', credential.password, (v) => setCredential({ ...credential, password: v }), 'password')}
              <Field>
                <FieldLabel htmlFor="credential-secret">{zh ? '特权密码（可选）' : 'Privilege secret (optional)'}</FieldLabel>
                <Input id="credential-secret" type="password" autoComplete="off" value={credential.secret} onChange={(event) => setCredential({ ...credential, secret: event.target.value })} />
              </Field>
            </FieldGroup>
            {failure('credentials')}
            <Button type="submit" className="self-start" disabled={busy}>{zh ? '保存凭据' : 'Save credential'}</Button>
          </form>
          <Separator />
          {rows('credentials', (data?.credentials ?? []).map((item) => {
            const target: EditTarget = { kind: 'credentials', item }
            return <AssetRow key={item.id} title={item.name} on={item.is_active} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
              detail={[item.username, item.description, labels.usedBy(switchesUsing(target, data))].filter(Boolean).join(' · ')} />
          }))}
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>{zh ? '命令模板' : 'Command profiles'}</CardTitle><CardDescription>{zh ? '交换机执行的命令和输出解析规则。' : 'The commands a switch runs and how their output is read.'}</CardDescription></CardHeader>
        <CardContent className="flex flex-col gap-6">
          <form noValidate className="flex flex-col gap-6" onSubmit={onSubmit(profileRequired, () => {
            try {
              void submit('profiles', Object.fromEntries(Object.entries(profile).map(([k, v]) => [k, k === 'name' ? v : JSON.parse(v) as unknown])), () => { setProfile(emptyProfile); profileRequired.reset() })
            } catch {
              setErrors((current) => ({ ...current, profiles: zh ? '模板中有不是合法 JSON 的内容。' : 'Part of the profile is not valid JSON.' }))
            }
          })}>
            <FieldGroup>
              {text(profileRequired, 'profile-name', name, profile.name, (v) => setProfile({ ...profile, name: v }))}
              {profileRuleKeys.map((key) => <Field key={key}>
                <FieldLabel htmlFor={`profile-${key}`}>{ruleLabel(key, zh)}</FieldLabel>
                <Textarea id={`profile-${key}`} className="font-mono" spellCheck={false} aria-describedby={`profile-${key}-hint`} rows={key === 'parser_rules' || key === 'command_templates' ? 4 : 2}
                  value={profile[key]} onChange={(event) => setProfile({ ...profile, [key]: event.target.value })} />
                <FieldDescription id={`profile-${key}-hint`}>{ruleHint(key, zh)}</FieldDescription>
              </Field>)}
            </FieldGroup>
            {failure('profiles')}
            <Button type="submit" className="self-start" disabled={busy}>{zh ? '保存模板' : 'Save profile'}</Button>
          </form>
          <Separator />
          {rows('profiles', (data?.profiles ?? []).map((item) => {
            const target: EditTarget = { kind: 'profiles', item }
            return <AssetRow key={item.id} title={item.name} on={item.is_active} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
              detail={labels.usedBy(switchesUsing(target, data))} />
          }))}
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>{zh ? '分组' : 'Groups'}</CardTitle><CardDescription>{labels.hint.groups}</CardDescription></CardHeader>
        <CardContent className="flex flex-col gap-6">
          <form noValidate className="flex flex-col gap-6" onSubmit={onSubmit(groupRequired, () => void submit('groups', group, () => { setGroup({ name: '', description: '' }); groupRequired.reset() }))}>
            <FieldGroup>
              {text(groupRequired, 'group-name', name, group.name, (v) => setGroup({ ...group, name: v }))}
              <Field>
                <FieldLabel htmlFor="group-description">{zh ? '描述（可选）' : 'Description (optional)'}</FieldLabel>
                <Input id="group-description" value={group.description} onChange={(event) => setGroup({ ...group, description: event.target.value })} />
              </Field>
            </FieldGroup>
            {failure('groups')}
            <Button type="submit" className="self-start" disabled={busy}>{zh ? '保存分组' : 'Save group'}</Button>
          </form>
          <Separator />
          {rows('groups', (data?.groups ?? []).map((item) => {
            const count = data?.switches.filter((s) => s.group_id === item.id).length ?? 0
            return <AssetRow key={item.id} title={item.name} on busy={busy} labels={labels} onEdit={() => setEditing({ kind: 'groups', item })}
              detail={[item.description, zh ? `${count} 台交换机` : `${count} switch${count === 1 ? '' : 'es'}`].filter(Boolean).join(' · ')} />
          }))}
        </CardContent>
      </Card>
      <Card>
        <CardHeader><CardTitle>{zh ? '网段' : 'Networks'}</CardTitle><CardDescription>{zh ? '网段决定一个 IP 由哪台交换机负责；只有已启用且已验证的网段参与解析。' : 'A network decides which switch owns an IP; only enabled, validated networks take part in resolution.'}</CardDescription></CardHeader>
        <CardContent className="flex flex-col gap-6">
          <form noValidate className="flex flex-col gap-6" onSubmit={onSubmit(networkRequired, () => void submit('networks', network, () => { setNetwork({ switch_id: '', cidr: '', is_validated: false }); networkRequired.reset() }))}>
            <FieldGroup>
              <Field data-invalid={networkRequired.invalid('network-switch')}>
                <FieldLabel htmlFor="network-switch">{zh ? '所属交换机' : 'Switch'}</FieldLabel>
                <Select value={network.switch_id} onValueChange={(value) => setNetwork({ ...network, switch_id: value })}>
                  <SelectTrigger id="network-switch" {...networkRequired.control('network-switch')}><SelectValue placeholder={labels.choose} /></SelectTrigger>
                  <SelectContent><SelectGroup>{data?.switches.map((item) => <SelectItem key={item.id} value={item.id}>{item.name} ({item.management_ip})</SelectItem>)}</SelectGroup></SelectContent>
                </Select>
                {networkRequired.message('network-switch')}
              </Field>
              <Field data-invalid={networkRequired.invalid('network-cidr')}>
                <FieldLabel htmlFor="network-cidr">IPv4 CIDR</FieldLabel>
                <Input id="network-cidr" className="font-mono" placeholder="10.0.0.0/24" value={network.cidr} onChange={(event) => setNetwork({ ...network, cidr: event.target.value })} {...networkRequired.control('network-cidr')} autoComplete="off" />
                {networkRequired.message('network-cidr')}
              </Field>
              <Field orientation="horizontal">
                <Checkbox id="network-validated" checked={network.is_validated} onCheckedChange={(checked) => setNetwork({ ...network, is_validated: checked === true })} />
                <FieldLabel htmlFor="network-validated" className="font-normal">{labels.validated}</FieldLabel>
              </Field>
            </FieldGroup>
            {failure('networks')}
            <Button type="submit" className="self-start" disabled={busy}>{zh ? '保存网段' : 'Save network'}</Button>
          </form>
          <Separator />
          <div className="max-h-[32rem] overflow-y-auto">{rows('networks', (data?.networks ?? []).map((item) => {
            const target: EditTarget = { kind: 'networks', item }
            return <AssetRow key={item.id} title={item.cidr} mono on={item.is_enabled} busy={busy} labels={labels} onEdit={() => setEditing(target)} onToggle={() => toggle(target)}
              flag={item.is_validated ? null : <UnvalidatedBadge labels={labels} />}
              detail={[labels.ownedBy(switchNames.get(item.switch_id) ?? '—'), item.vlan ? `VLAN ${item.vlan}` : '', item.description].filter(Boolean).join(' · ')} />
          }))}</div>
        </CardContent>
      </Card>
    </div>
    <AssetEditDialog target={editing} inventory={data} zh={zh} onClose={() => setEditing(null)} onSave={saveEdit} />
    <DisableAssetDialog target={disabling} inventory={data} zh={zh} onCancel={() => setDisabling(null)} onConfirm={(target) => void setStatus(target, false)} />
  </main>
}
