import { createFileRoute } from '@tanstack/react-router'
import { Plus } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { AdminOnly, Empty, ErrorNote, PageHeader, ToneBadge, pick } from '@/features/ops/ui'
import { switchesUsing, type EditTarget } from '@/features/release/asset-editors'
import { AssetActions, CreateAssetDialog, useAssetPage } from '@/features/release/asset-page'
import { errorText } from '@/features/release/messages'
import { useRequiredFields } from '@/features/release/required-fields'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/_authenticated/credentials')({ component: CredentialsPage })

const blank = { name: '', username: '', password: '', secret: '' }

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
    <PageHeader title={pick(zh, '凭据', 'Credentials')}>
      <Button type="button" onClick={() => { setError(''); setCreating(true) }}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '新建凭据', 'New credential')}</Button>
    </PageHeader>
    <ErrorNote error={page.inventory.error} zh={zh} />
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
