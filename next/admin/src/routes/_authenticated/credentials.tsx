import { createFileRoute } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { AdminOnly, Empty, ErrorNote, PageHeader, ToneBadge, pick } from '@/features/ops/ui'
import { SnmpCredentialFields, snmpCredentialBody, snmpProtocolName, switchesUsing, type EditTarget, type SnmpCredential } from '@/features/release/asset-editors'
import { AssetActions, CreateAssetDialog, useAssetPage } from '@/features/release/asset-page'
import { errorText } from '@/features/release/messages'
import { useRequiredFields } from '@/features/release/required-fields'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/_authenticated/credentials')({ component: CredentialsPage })

const blank = { name: '', username: '', password: '', secret: '' }
const blankSnmp = { name: '', description: '', snmp_version: 'v2c', community: '', username: '', auth_protocol: 'sha', auth_password: '', priv_protocol: 'aes', priv_password: '' }

function CredentialsPage() {
  const page = useAssetPage()
  const { zh, labels, data } = page
  const [creating, setCreating] = useState(false)
  const [form, setForm] = useState(blank)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const label = { username: pick(zh, '设备用户名', 'Device username'), password: pick(zh, '设备密码', 'Device password') }
  const required = useRequiredFields(zh, [
    { id: 'credential-name', label: labels.name, value: form.name },
    { id: 'credential-username', label: label.username, value: form.username },
    { id: 'credential-password', label: label.password, value: form.password },
  ])
  if (!page.admin) return <AdminOnly zh={zh} />

  async function save() {
    if (!required.check()) return
    setSaving(true); setError('')
    try { await page.create('credentials', form); setCreating(false); setForm(blank); required.reset() }
    catch (cause) { setError(errorText(cause, zh)) }
    finally { setSaving(false) }
  }
  const text = (id: string, key: keyof typeof blank, fieldLabel: string, type = 'text') =>
    <Field data-invalid={required.invalid(id)}>
      <FieldLabel htmlFor={id}>{fieldLabel}</FieldLabel>
      <Input id={id} type={type} value={form[key]} onChange={(event) => setForm({ ...form, [key]: event.target.value })} {...required.control(id)} autoComplete={type === 'password' ? 'new-password' : 'off'} />
      {required.message(id)}
    </Field>
  const credentials = data?.credentials ?? []

  return <main className="mx-auto flex w-full max-w-5xl flex-col gap-6">
    <PageHeader title={pick(zh, '凭据', 'Credentials')} />
    <ErrorNote error={page.inventory.error} zh={zh} />
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="text-base font-semibold">{pick(zh, '登录凭据', 'Login credentials')}</h2>
          <p className="text-sm text-muted-foreground">{pick(zh, '登录交换机解绑、备份和读取网段用的账号。', 'Accounts for signing in to switches: releases, backups and network reads.')}</p>
        </div>
        <Button type="button" onClick={() => { setError(''); setCreating(true) }}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '新建凭据', 'New credential')}</Button>
      </div>
    {data && !credentials.length
      ? <Empty>{labels.empty.credentials}</Empty>
      : <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{labels.name}</TableHead><TableHead>{label.username}</TableHead><TableHead>{pick(zh, '特权密码', 'Privilege secret')}</TableHead>
            <TableHead>{pick(zh, '使用情况', 'In use')}</TableHead><TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>{credentials.map((item) => {
            const target: EditTarget = { kind: 'credentials', item }
            return <TableRow key={item.id}>
              <TableCell>
                <div className="flex flex-wrap items-center gap-2"><span className={cn('font-medium', !item.is_active && 'text-muted-foreground')}>{item.name}</span>{item.is_active ? null : <ToneBadge kind="muted">{labels.disabled}</ToneBadge>}</div>
                {item.description ? <p className="text-xs text-muted-foreground">{item.description}</p> : null}
              </TableCell>
              <TableCell className="font-mono text-sm">{item.username}</TableCell>
              <TableCell className="text-sm">{item.has_secret ? pick(zh, '已设置', 'Set') : <span className="text-muted-foreground">—</span>}</TableCell>
              <TableCell className="text-sm text-muted-foreground">{labels.usedBy(switchesUsing(target, data))}</TableCell>
              <TableCell><AssetActions title={item.name} on={item.is_active} busy={page.busy} labels={labels} onEdit={() => page.edit(target)} onToggle={() => page.toggle(target)} /></TableCell>
            </TableRow>
          })}</TableBody>
        </Table>
      </div>}
    </section>
    <SnmpSection page={page} />
    <CreateAssetDialog open={creating} zh={zh} title={pick(zh, '新建凭据', 'New credential')} error={error} saving={saving} onClose={() => setCreating(false)} onSubmit={() => void save()}
      description={pick(zh, '登录交换机用的账号。密码加密保存，保存后不再显示。', 'An account for signing in to switches. Passwords are stored encrypted and never shown again.')}>
      <FieldGroup>
        {text('credential-name', 'name', labels.name)}
        {text('credential-username', 'username', label.username)}
        {text('credential-password', 'password', label.password, 'password')}
        <Field>
          <FieldLabel htmlFor="credential-secret">{pick(zh, '特权密码（可选）', 'Privilege secret (optional)')}</FieldLabel>
          <Input id="credential-secret" type="password" autoComplete="new-password" aria-describedby="credential-secret-hint" value={form.secret} onChange={(event) => setForm({ ...form, secret: event.target.value })} />
          <FieldDescription id="credential-secret-hint">{pick(zh, '登录后进入特权模式要再输的密码，比如华为、H3C 的 super 或锐捷的 enable。', 'The extra password some switches ask for privileged mode, such as super on Huawei and H3C or enable on Ruijie.')}</FieldDescription>
        </Field>
      </FieldGroup>
    </CreateAssetDialog>
    {page.dialogs}
  </main>
}

/** SNMP credentials let the worker poll switches for monitoring; they never sign in. */
function SnmpSection({ page }: { page: ReturnType<typeof useAssetPage> }) {
  const { zh, labels, data } = page
  const [creating, setCreating] = useState(false)
  const [form, setForm] = useState<Record<string, string>>(blankSnmp)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const v3 = form.snmp_version === 'v3'
  const required = useRequiredFields(zh, [
    { id: 'new-snmp-name', label: labels.name, value: form.name },
    v3 ? { id: 'new-snmp-user', label: labels.snmpUser, value: form.username } : { id: 'new-snmp-community', label: labels.community, value: form.community },
  ])
  async function save() {
    if (!required.check()) return
    setSaving(true); setError('')
    try { await page.create('snmp_credentials', snmpCredentialBody(form)); setCreating(false); setForm(blankSnmp); required.reset() }
    catch (cause) { setError(errorText(cause, zh)) }
    finally { setSaving(false) }
  }
  const security = (item: SnmpCredential) => {
    if (item.snmp_version === 'v2c') return pick(zh, '团体字', 'Community')
    if (!item.auth_protocol) return pick(zh, `${item.username} · 不认证`, `${item.username} · no authentication`)
    return `${item.username} · ${snmpProtocolName(item.auth_protocol)}${item.priv_protocol ? ` + ${snmpProtocolName(item.priv_protocol)}` : pick(zh, ' · 不加密', ' · no privacy')}`
  }
  const items = data?.snmp_credentials ?? []
  return <section className="flex flex-col gap-3">
    <div className="flex flex-wrap items-end justify-between gap-2">
      <div>
        <h2 className="text-base font-semibold">{pick(zh, 'SNMP 凭据', 'SNMP credentials')}</h2>
        <p className="text-sm text-muted-foreground">{pick(zh, '设备监控用，只读取 CPU、内存、温度和端口状态。在交换机的“编辑”里或设备页批量选择。', 'For device monitoring; polls only read CPU, memory, temperature and ports. Give one to switches under Edit or in bulk on the devices page.')}</p>
      </div>
      <Button type="button" variant="outline" onClick={() => { setError(''); setCreating(true) }}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '新建 SNMP 凭据', 'New SNMP credential')}</Button>
    </div>
    {data && !items.length
      ? <Empty>{labels.empty.snmp_credentials}</Empty>
      : <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{labels.name}</TableHead><TableHead>{pick(zh, '版本', 'Version')}</TableHead><TableHead>{pick(zh, '用户与安全', 'User and security')}</TableHead>
            <TableHead>{pick(zh, '使用情况', 'In use')}</TableHead><TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>{items.map((item) => {
            const target: EditTarget = { kind: 'snmp_credentials', item }
            return <TableRow key={item.id}>
              <TableCell>
                <div className="flex flex-wrap items-center gap-2"><span className={cn('font-medium', !item.is_active && 'text-muted-foreground')}>{item.name}</span>{item.is_active ? null : <ToneBadge kind="muted">{labels.disabled}</ToneBadge>}</div>
                {item.description ? <p className="text-xs text-muted-foreground">{item.description}</p> : null}
              </TableCell>
              <TableCell className="text-sm">{item.snmp_version}</TableCell>
              <TableCell className="text-sm">{security(item)}</TableCell>
              <TableCell className="text-sm text-muted-foreground">{labels.usedBy(switchesUsing(target, data))}</TableCell>
              <TableCell><AssetActions title={item.name} on={item.is_active} busy={page.busy} labels={labels} onEdit={() => page.edit(target)} onToggle={() => page.toggle(target)} /></TableCell>
            </TableRow>
          })}</TableBody>
        </Table>
      </div>}
    <CreateAssetDialog open={creating} zh={zh} title={pick(zh, '新建 SNMP 凭据', 'New SNMP credential')} error={error} saving={saving} onClose={() => setCreating(false)} onSubmit={() => void save()}
      description={pick(zh, '团体字和密码加密保存，保存后不再显示。交换机上只需要只读权限。', 'Communities and passwords are stored encrypted and never shown again. Read-only access on the switch is enough.')}>
      <FieldGroup>
        <SnmpCredentialFields idPrefix="new" form={form} labels={labels} required={required} onChange={(key, value) => setForm({ ...form, [key]: value })} />
      </FieldGroup>
    </CreateAssetDialog>
  </section>
}
