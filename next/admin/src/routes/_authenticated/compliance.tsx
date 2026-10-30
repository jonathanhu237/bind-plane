import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { CircleAlert, Pencil, Plus } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { opsRequest, type ComplianceResult, type Page, type Rule } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, Pager, Section, SeverityBadge, ToneBadge, Time, isAdmin, pick, useZh } from '@/features/ops/ui'
import { callAssets, vendorName, vendors, type Inventory } from '@/features/release/asset-editors'
import { errorText, isConfigExcerpt, serverText } from '@/features/release/messages'
import { useRequiredFields } from '@/features/release/required-fields'

export const Route = createFileRoute('/_authenticated/compliance')({ component: CompliancePage })

type Summary = { passed: number; failed: number; switches_evaluated: number; switches_failing: number }
// "Every vendor", "every group" and "every rule" travel as this select value and are sent as ''.
const ALL = 'all'

function RuleDialog({ rule, zh, inventory, onClose, onSaved }: { rule: Rule | null; zh: boolean; inventory?: Inventory; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState({
    name: rule?.name ?? '', description: rule?.description ?? '', pattern: rule?.pattern ?? '', mode: rule?.mode ?? 'must_contain',
    severity: rule?.severity ?? 'warning', vendor: rule?.vendor ?? '', group_id: rule?.group_id ?? '', is_enabled: rule?.is_enabled ?? true,
  })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const required = useRequiredFields(zh, [
    { id: 'rule-name', label: pick(zh, '名称', 'Name'), value: form.name },
    { id: 'rule-pattern', label: pick(zh, '匹配规则', 'Pattern'), value: form.pattern },
  ])
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!required.check()) return
    setBusy(true); setError('')
    try {
      if (rule) await opsRequest(`compliance/rules/${rule.id}`, { ...form, version: rule.version }, 'PATCH')
      else await opsRequest('compliance/rules', form)
      onSaved()
    } catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }
  const choice = <K extends 'mode' | 'severity' | 'vendor' | 'group_id'>(id: string, key: K, label: string, options: { value: string; label: string }[]) =>
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Select value={form[key] || ALL} onValueChange={(value) => setForm({ ...form, [key]: (value === ALL ? '' : value) as (typeof form)[K] })}>
        <SelectTrigger id={id}><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>{options.map((option) => <SelectItem key={option.value} value={option.value}>{option.label}</SelectItem>)}</SelectGroup></SelectContent>
      </Select>
    </Field>
  return <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
      <form className="flex flex-col gap-6" noValidate onSubmit={(event) => void submit(event)}>
        <DialogHeader>
          <DialogTitle>{rule ? pick(zh, '编辑合规规则', 'Edit compliance rule') : pick(zh, '新建合规规则', 'New compliance rule')}</DialogTitle>
          <DialogDescription>{pick(zh, '规则用正则表达式（RE2 语法，^ 和 $ 匹配每一行）检查每台设备的最新配置。只有“严重”级别的不合规会产生告警。', 'A rule checks each switch’s latest configuration with a regular expression (RE2 syntax; ^ and $ match each line). Only critical failures raise alerts.')}</DialogDescription>
        </DialogHeader>
        <FieldGroup>
          <Field data-invalid={required.invalid('rule-name')}>
            <FieldLabel htmlFor="rule-name">{pick(zh, '名称', 'Name')}</FieldLabel>
            <Input id="rule-name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} {...required.control('rule-name')} />
            {required.message('rule-name')}
          </Field>
          <Field data-invalid={required.invalid('rule-pattern')}>
            <FieldLabel htmlFor="rule-pattern">{pick(zh, '匹配规则', 'Pattern')}</FieldLabel>
            <Textarea id="rule-pattern" className="font-mono" rows={3} spellCheck={false} placeholder="^ntp-service unicast-server 10\.0\.0\.1"
              value={form.pattern} onChange={(event) => setForm({ ...form, pattern: event.target.value })} {...required.control('rule-pattern')} />
            {required.message('rule-pattern')}
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            {choice('rule-mode', 'mode', pick(zh, '判定方式', 'Passes when'), [
              { value: 'must_contain', label: pick(zh, '配置中必须出现', 'the pattern is present') }, { value: 'must_not_contain', label: pick(zh, '配置中不得出现', 'the pattern is absent') },
            ])}
            {choice('rule-severity', 'severity', pick(zh, '严重级别', 'Severity'), [
              { value: 'info', label: pick(zh, '提示', 'Info') }, { value: 'warning', label: pick(zh, '警告', 'Warning') }, { value: 'critical', label: pick(zh, '严重（产生告警）', 'Critical (raises alerts)') },
            ])}
            {choice('rule-vendor', 'vendor', pick(zh, '适用厂商', 'Vendor'), [{ value: ALL, label: pick(zh, '全部厂商', 'All vendors') }, ...vendors.map((v) => ({ value: v, label: vendorName(v, zh) }))])}
            {choice('rule-group', 'group_id', pick(zh, '适用分组', 'Group'), [{ value: ALL, label: pick(zh, '全部分组', 'All groups') }, ...(inventory?.groups ?? []).map((g) => ({ value: g.id, label: g.name }))])}
          </div>
          <Field>
            <FieldLabel htmlFor="rule-description">{pick(zh, '说明（可选）', 'Description (optional)')}</FieldLabel>
            <Input id="rule-description" value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} />
          </Field>
          <Field orientation="horizontal">
            <Checkbox id="rule-enabled" checked={form.is_enabled} onCheckedChange={(checked) => setForm({ ...form, is_enabled: checked === true })} />
            <FieldLabel htmlFor="rule-enabled" className="font-normal">{pick(zh, '启用', 'Enabled')}</FieldLabel>
          </Field>
        </FieldGroup>
        {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
        <DialogFooter><Button type="button" variant="outline" onClick={onClose}>{pick(zh, '取消', 'Cancel')}</Button><Button type="submit" disabled={busy}>{busy ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '保存', 'Save')}</Button></DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}

function CompliancePage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
  const [status, setStatus] = useState('failed')
  const [ruleFilter, setRuleFilter] = useState('')
  const [q, setQ] = useState('')
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<Rule | null | 'new'>(null)
  const rules = useQuery({ queryKey: ['ops-rules'], queryFn: () => opsRequest<Rule[]>('compliance/rules'), enabled: admin, refetchInterval: 15_000 })
  const params = new URLSearchParams({ status, rule: ruleFilter, q: q.trim(), page: String(page) })
  const results = useQuery({ queryKey: ['ops-results', params.toString()], queryFn: () => opsRequest<Page<ComplianceResult> & { summary: Summary }>(`compliance/results?${params}`), enabled: admin, placeholderData: keepPreviousData, refetchInterval: 15_000 })
  const inventory = useQuery({ queryKey: ['release-assets', user.id], queryFn: () => callAssets<Inventory>('assets'), enabled: admin, staleTime: 60_000 })
  if (!admin) return <AdminOnly zh={zh} />
  const summary = results.data?.summary
  const rate = summary && summary.passed + summary.failed ? Math.round((summary.passed / (summary.passed + summary.failed)) * 1000) / 10 : null
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-8">
    <PageHeader title={pick(zh, '合规检查', 'Compliance')} description={pick(zh, '用基线规则检查每台设备的最新配置。新的备份或规则改动会在几十秒内重新评估。', 'Baseline rules checked against each switch’s latest configuration. New backups and rule changes are re-evaluated within seconds.')}>
      <Button type="button" onClick={() => setEditing('new')}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '新建规则', 'New rule')}</Button>
    </PageHeader>
    <ErrorNote error={rules.error ?? results.error} zh={zh} />
    {summary ? <Card><CardContent className="pt-6"><dl className="grid gap-4 sm:grid-cols-4">
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '检查通过率', 'Pass rate')}</dt><dd className="text-lg font-semibold tabular-nums">{rate === null ? '—' : `${rate}%`}</dd></div>
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '未通过的设备', 'Switches failing')}</dt><dd className="text-lg font-semibold tabular-nums">{summary.switches_failing} <span className="text-sm font-normal text-muted-foreground">/ {summary.switches_evaluated}</span></dd></div>
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '未通过的检查项', 'Failed checks')}</dt><dd className="text-lg font-semibold tabular-nums">{summary.failed}</dd></div>
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '通过的检查项', 'Passed checks')}</dt><dd className="text-lg font-semibold tabular-nums">{summary.passed}</dd></div>
    </dl></CardContent></Card> : null}
    <Section title={pick(zh, '规则', 'Rules')}>
      {rules.data?.length ? <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap"><Table>
        <TableHeader><TableRow><TableHead>{pick(zh, '名称', 'Name')}</TableHead><TableHead>{pick(zh, '匹配规则', 'Pattern')}</TableHead><TableHead>{pick(zh, '级别', 'Severity')}</TableHead><TableHead>{pick(zh, '范围', 'Scope')}</TableHead><TableHead className="text-right">{pick(zh, '通过 / 未通过', 'Pass / fail')}</TableHead><TableHead /></TableRow></TableHeader>
        <TableBody>{rules.data.map((rule) => <TableRow key={rule.id}>
          <TableCell><div className="flex items-center gap-2 whitespace-nowrap font-medium">{rule.name}{rule.is_enabled ? null : <ToneBadge kind="muted">{pick(zh, '已停用', 'Disabled')}</ToneBadge>}</div>
            <p className="text-xs text-muted-foreground">{rule.mode === 'must_contain' ? pick(zh, '必须出现', 'Must be present') : pick(zh, '不得出现', 'Must be absent')}</p></TableCell>
          <TableCell className="max-w-72"><code className="block truncate font-mono text-xs" title={rule.pattern}>{rule.pattern}</code></TableCell>
          <TableCell><SeverityBadge severity={rule.severity} zh={zh} /></TableCell>
          <TableCell className="whitespace-nowrap text-sm">{[rule.vendor ? vendorName(rule.vendor, zh) : null, rule.group_name].filter(Boolean).join(' · ') || pick(zh, '全部', 'All')}</TableCell>
          <TableCell className="text-right tabular-nums">{rule.passed} / <span className={rule.failed ? 'text-destructive' : ''}>{rule.failed}</span></TableCell>
          <TableCell className="text-right"><Button type="button" variant="ghost" size="sm" aria-label={`${pick(zh, '编辑', 'Edit')} ${rule.name}`} onClick={() => setEditing(rule)}><Pencil aria-hidden="true" /></Button></TableCell>
        </TableRow>)}</TableBody>
      </Table></div> : <Empty>{pick(zh, '还没有规则。例如：“必须配置 NTP 服务器”“不得使用 telnet server enable”。', 'No rules yet. For example: an NTP server must be configured, or telnet server enable must be absent.')}</Empty>}
    </Section>
    <Section title={pick(zh, '检查结果', 'Results')} actions={<>
      <Select value={status} onValueChange={(value) => { setStatus(value); setPage(1) }}>
        <SelectTrigger aria-label={pick(zh, '结果', 'Result')} className="w-auto min-w-32"><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value="failed">{pick(zh, '仅未通过', 'Failing only')}</SelectItem><SelectItem value="passed">{pick(zh, '仅通过', 'Passing only')}</SelectItem><SelectItem value="all">{pick(zh, '全部', 'All')}</SelectItem>
        </SelectGroup></SelectContent>
      </Select>
      <Select value={ruleFilter || ALL} onValueChange={(value) => { setRuleFilter(value === ALL ? '' : value); setPage(1) }}>
        <SelectTrigger aria-label={pick(zh, '规则', 'Rule')} className="w-auto min-w-32"><SelectValue /></SelectTrigger>
        <SelectContent><SelectGroup>
          <SelectItem value={ALL}>{pick(zh, '全部规则', 'All rules')}</SelectItem>
          {rules.data?.map((rule) => <SelectItem key={rule.id} value={rule.id}>{rule.name}</SelectItem>)}
        </SelectGroup></SelectContent>
      </Select>
      <Input aria-label={pick(zh, '搜索设备', 'Search switches')} placeholder={pick(zh, '搜索设备', 'Search switches')} value={q} onChange={(event) => { setQ(event.target.value); setPage(1) }} className="w-44" />
    </>}>
      {results.data?.items.length ? <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap"><Table>
        <TableHeader><TableRow><TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '规则', 'Rule')}</TableHead><TableHead>{pick(zh, '结果', 'Result')}</TableHead><TableHead>{pick(zh, '依据', 'Evidence')}</TableHead><TableHead>{pick(zh, '评估时间', 'Evaluated')}</TableHead></TableRow></TableHeader>
        <TableBody>{results.data.items.map((result) => {
          const evidence = serverText(result.detail, zh)
          return <TableRow key={`${result.switch_id}:${result.rule_id}`}>
            <TableCell className="whitespace-nowrap"><span className="font-medium">{result.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{result.management_ip}</span></TableCell>
            <TableCell><div className="flex items-center gap-2 whitespace-nowrap">{result.rule_name}<SeverityBadge severity={result.severity} zh={zh} /></div></TableCell>
            <TableCell><ToneBadge kind={result.passed ? 'good' : 'bad'}>{result.passed ? pick(zh, '通过', 'Pass') : pick(zh, '未通过', 'Fail')}</ToneBadge></TableCell>
            <TableCell className={cn('min-w-48 max-w-80 break-words text-xs text-muted-foreground', isConfigExcerpt(result.detail) && 'font-mono')}>{evidence || '—'}</TableCell>
            <TableCell className="whitespace-nowrap"><Time value={result.evaluated_at} zh={zh} className="text-xs" /></TableCell>
          </TableRow>
        })}</TableBody>
      </Table></div> : <Empty>{status === 'failed' ? pick(zh, '没有未通过的检查。', 'Nothing is failing.') : pick(zh, '没有结果。规则需要设备至少有一次配置备份才能评估。', 'No results. Rules are evaluated once a switch has at least one configuration backup.')}</Empty>}
      {results.data ? <Pager page={page} total={results.data.total} pageSize={results.data.page_size} onPage={setPage} zh={zh} /> : null}
    </Section>
    {editing !== null ? <RuleDialog key={editing === 'new' ? 'new' : `${editing.id}:${editing.version}`} rule={editing === 'new' ? null : editing} zh={zh} inventory={inventory.data}
      onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void rules.refetch(); void results.refetch() }} /> : null}
  </main>
}
