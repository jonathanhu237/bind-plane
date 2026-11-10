import { createFileRoute } from '@tanstack/react-router'
import { ChevronDown, Plus } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { AdminOnly, Empty, ErrorNote, PageHeader, ToneBadge, pick } from '@/features/ops/ui'
import {
  profileRuleKeys, ruleHint, ruleLabel, switchesUsing, vendorName, vendors, type EditTarget, type Profile,
} from '@/features/release/asset-editors'
import { AssetActions, CreateAssetDialog, useAssetPage } from '@/features/release/asset-page'
import { errorText } from '@/features/release/messages'
import { useRequiredFields } from '@/features/release/required-fields'
import { cn } from '@/lib/utils'

export const Route = createFileRoute('/_authenticated/command-profiles')({ component: CommandProfilesPage })

type RuleKey = (typeof profileRuleKeys)[number]
type ProfileForm = { name: string; vendor: string } & Record<RuleKey, string>

// What a release needs comes first; the rest rarely changes from its default.
const mainRules: RuleKey[] = ['command_templates', 'parser_rules']
const moreRules = profileRuleKeys.filter((key) => !mainRules.includes(key))
const arrayRules = new Set<RuleKey>(['success_patterns', 'error_patterns'])
const blank: ProfileForm = {
  name: '', vendor: '',
  command_templates: '{\n  "single_arp_query": "",\n  "arp_release": "",\n  "config_backup": "display current-configuration"\n}',
  parser_rules: '{\n  "arp_entry_regex": "",\n  "static_type_values": [],\n  "dynamic_type_values": [],\n  "missing_patterns": []\n}',
  login_prompt_patterns: '{}', prompt_patterns: '{}', pagination_rules: '{}', success_patterns: '[]', error_patterns: '[]',
}
const vendorOrder = (vendor: string) => vendors.indexOf(vendor as (typeof vendors)[number])
const queryCommand = (profile: Profile) => {
  const templates = profile.command_templates
  return templates && typeof templates === 'object' ? String((templates as Record<string, unknown>).single_arp_query ?? '') : ''
}

function CommandProfilesPage() {
  const page = useAssetPage()
  const { zh, labels, data } = page
  const [creating, setCreating] = useState<{ basedOn?: string } | null>(null)
  const [form, setForm] = useState<ProfileForm>(blank)
  const [more, setMore] = useState(false)
  const [invalid, setInvalid] = useState<RuleKey | ''>('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const required = useRequiredFields(zh, [
    { id: 'profile-name', label: labels.name, value: form.name },
    { id: 'profile-vendor', label: labels.vendor, value: form.vendor, choose: true },
  ])
  if (!page.admin) return <AdminOnly zh={zh} />

  function open(next: ProfileForm, basedOn?: string) {
    setForm(next); setMore(false); setInvalid(''); setError(''); required.reset(); setCreating({ basedOn })
  }
  // Copying starts a new profile from another's content, a quick way to make a variant of it.
  function copy(item: Profile) {
    open({
      name: zh ? `${item.name}（副本）` : `${item.name} copy`, vendor: item.vendor,
      ...Object.fromEntries(profileRuleKeys.map((key) => [key, JSON.stringify(item[key] ?? (arrayRules.has(key) ? [] : {}), null, 2)])) as Record<RuleKey, string>,
    }, item.name)
  }
  async function save() {
    if (!required.check()) return
    const body: Record<string, unknown> = { name: form.name, vendor: form.vendor }
    for (const key of profileRuleKeys) {
      let parsed: unknown
      try { parsed = JSON.parse(form[key]) } catch { parsed = undefined }
      if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed) !== arrayRules.has(key)) {
        setInvalid(key); setError(labels.invalidRule(key))
        if (moreRules.includes(key)) setMore(true)
        return
      }
      body[key] = parsed
    }
    setSaving(true); setError(''); setInvalid('')
    try { await page.create('profiles', body); setCreating(null) }
    catch (cause) { setError(errorText(cause, zh)) }
    finally { setSaving(false) }
  }
  const rule = (key: RuleKey, rows: number) =>
    <Field key={key} data-invalid={invalid === key}>
      <FieldLabel htmlFor={`profile-${key}`}>{ruleLabel(key, zh)}</FieldLabel>
      <Textarea id={`profile-${key}`} className="font-mono text-xs" spellCheck={false} rows={rows} aria-describedby={`profile-${key}-hint`} aria-invalid={invalid === key || undefined}
        value={form[key]} onChange={(event) => setForm({ ...form, [key]: event.target.value })} />
      <FieldDescription id={`profile-${key}-hint`}>{ruleHint(key, zh)}</FieldDescription>
    </Field>
  const profiles = [...(data?.profiles ?? [])].sort((a, b) => vendorOrder(a.vendor) - vendorOrder(b.vendor) || a.name.localeCompare(b.name, zh ? 'zh-CN' : 'en'))

  return <main className="mx-auto flex w-full max-w-5xl flex-col gap-6">
    <PageHeader title={pick(zh, '命令模板', 'Command profiles')}>
      <Button type="button" onClick={() => open(blank)}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '新建模板', 'New profile')}</Button>
    </PageHeader>
    <ErrorNote error={page.inventory.error} zh={zh} />
    {data && !profiles.length
      ? <Empty>{labels.empty.profiles}</Empty>
      : <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap">
        <Table>
          <TableHeader><TableRow>
            <TableHead>{labels.name}</TableHead><TableHead>{labels.vendor}</TableHead><TableHead>{pick(zh, '查询命令', 'Query command')}</TableHead>
            <TableHead>{pick(zh, '使用情况', 'In use')}</TableHead><TableHead className="text-right">{pick(zh, '操作', 'Actions')}</TableHead>
          </TableRow></TableHeader>
          <TableBody>{profiles.map((item) => {
            const target: EditTarget = { kind: 'profiles', item }
            return <TableRow key={item.id}>
              <TableCell><div className="flex flex-wrap items-center gap-2"><span className={cn('font-medium', !item.is_active && 'text-muted-foreground')}>{item.name}</span>{item.is_active ? null : <ToneBadge kind="muted">{labels.disabled}</ToneBadge>}</div></TableCell>
              <TableCell className="whitespace-nowrap text-sm">{vendorName(item.vendor, zh)}</TableCell>
              <TableCell className="max-w-64 truncate font-mono text-xs" title={queryCommand(item)}>{queryCommand(item) || <span className="text-muted-foreground">—</span>}</TableCell>
              <TableCell className="whitespace-nowrap text-sm text-muted-foreground">{labels.usedBy(switchesUsing(target, data))}</TableCell>
              <TableCell><AssetActions title={item.name} on={item.is_active} busy={page.busy} labels={labels} onEdit={() => page.edit(target)} onCopy={() => copy(item)} onToggle={() => page.toggle(target)} /></TableCell>
            </TableRow>
          })}</TableBody>
        </Table>
      </div>}
    <CreateAssetDialog open={creating !== null} zh={zh} wide title={pick(zh, '新建命令模板', 'New command profile')} error={error} saving={saving} onClose={() => setCreating(null)} onSubmit={() => void save()}
      description={creating?.basedOn
        ? pick(zh, `以「${creating.basedOn}」为基础，修改后保存为新模板。`, `Based on “${creating.basedOn}”; change it and save it as a new profile.`)
        : pick(zh, '模板只能给同一厂商的交换机使用，一个厂商可以有多个模板。', 'Only switches of the vendor can use the profile; a vendor can have several.')}>
      <FieldGroup>
        <div className="grid gap-4 sm:grid-cols-2">
          <Field data-invalid={required.invalid('profile-name')}>
            <FieldLabel htmlFor="profile-name">{labels.name}</FieldLabel>
            <Input id="profile-name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} {...required.control('profile-name')} autoComplete="off" />
            {required.message('profile-name')}
          </Field>
          <Field data-invalid={required.invalid('profile-vendor')}>
            <FieldLabel htmlFor="profile-vendor">{labels.vendor}</FieldLabel>
            <Select value={form.vendor} onValueChange={(value) => setForm({ ...form, vendor: value })}>
              <SelectTrigger id="profile-vendor" {...required.control('profile-vendor')}><SelectValue placeholder={labels.choose} /></SelectTrigger>
              <SelectContent><SelectGroup>{vendors.map((vendor) => <SelectItem key={vendor} value={vendor}>{vendorName(vendor, zh)}</SelectItem>)}</SelectGroup></SelectContent>
            </Select>
            {required.message('profile-vendor')}
          </Field>
        </div>
        {mainRules.map((key) => rule(key, 7))}
        <Collapsible open={more} onOpenChange={setMore}>
          <CollapsibleTrigger asChild>
            <Button type="button" variant="ghost" size="sm" className="-ml-2 self-start text-muted-foreground">
              <ChevronDown aria-hidden="true" data-icon="inline-start" className={cn('transition-transform', !more && '-rotate-90')} />
              {pick(zh, '更多设置：提示符、分页、成功与错误匹配', 'More: prompts, paging, success and error patterns')}
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="flex flex-col gap-6 pt-4">{moreRules.map((key) => rule(key, 3))}</CollapsibleContent>
        </Collapsible>
      </FieldGroup>
    </CreateAssetDialog>
    {page.dialogs}
  </main>
}
