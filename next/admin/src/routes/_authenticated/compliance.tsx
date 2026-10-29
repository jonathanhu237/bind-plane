import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { Pencil, Plus } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { opsRequest, type ComplianceResult, type Page, type Rule, type Severity } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, Pager, Section, SeverityBadge, ToneBadge, Time, isAdmin, pick, useZh } from '@/features/ops/ui'
import { assetErrorText, callAssets, selectClass, vendorName, vendors, type Inventory } from '@/features/release/asset-editors'

export const Route = createFileRoute('/_authenticated/compliance')({ component: CompliancePage })

type Summary = { passed: number; failed: number; switches_evaluated: number; switches_failing: number }

function RuleDialog({ rule, zh, inventory, onClose, onSaved }: { rule: Rule | null; zh: boolean; inventory?: Inventory; onClose: () => void; onSaved: () => void }) {
  const [form, setForm] = useState({
    name: rule?.name ?? '', description: rule?.description ?? '', pattern: rule?.pattern ?? '', mode: rule?.mode ?? 'must_contain',
    severity: rule?.severity ?? 'warning', vendor: rule?.vendor ?? '', group_id: rule?.group_id ?? '', is_enabled: rule?.is_enabled ?? true,
  })
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true); setError('')
    try {
      if (rule) await opsRequest(`compliance/rules/${rule.id}`, { ...form, version: rule.version }, 'PATCH')
      else await opsRequest('compliance/rules', form)
      onSaved()
    } catch (cause) { setError(assetErrorText(cause, zh)) }
    finally { setBusy(false) }
  }
  return <Dialog open onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
      <form className="space-y-4" onSubmit={(event) => void submit(event)}>
        <DialogHeader>
          <DialogTitle>{rule ? pick(zh, '编辑合规规则', 'Edit compliance rule') : pick(zh, '新建合规规则', 'New compliance rule')}</DialogTitle>
          <DialogDescription>{pick(zh, '规则用正则表达式（RE2 语法，^ 和 $ 匹配每一行）检查每台设备的最新配置。只有“严重”级别的不合规会产生告警。', 'A rule checks each switch’s latest configuration with a regular expression (RE2 syntax; ^ and $ match each line). Only critical failures raise alerts.')}</DialogDescription>
        </DialogHeader>
        <div className="space-y-2"><Label htmlFor="rule-name">{pick(zh, '名称', 'Name')}</Label><Input id="rule-name" required value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} /></div>
        <div className="space-y-2"><Label htmlFor="rule-pattern">{pick(zh, '匹配规则', 'Pattern')}</Label>
          <textarea id="rule-pattern" required rows={3} spellCheck={false} value={form.pattern} onChange={(event) => setForm({ ...form, pattern: event.target.value })} placeholder="^ntp-service unicast-server 10\.0\.0\.1" className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" /></div>
        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2"><Label htmlFor="rule-mode">{pick(zh, '判定方式', 'Passes when')}</Label>
            <select id="rule-mode" className={selectClass} value={form.mode} onChange={(event) => setForm({ ...form, mode: event.target.value as Rule['mode'] })}>
              <option value="must_contain">{pick(zh, '配置中必须出现', 'the pattern is present')}</option><option value="must_not_contain">{pick(zh, '配置中不得出现', 'the pattern is absent')}</option>
            </select></div>
          <div className="space-y-2"><Label htmlFor="rule-severity">{pick(zh, '严重级别', 'Severity')}</Label>
            <select id="rule-severity" className={selectClass} value={form.severity} onChange={(event) => setForm({ ...form, severity: event.target.value as Severity })}>
              <option value="info">{pick(zh, '提示', 'Info')}</option><option value="warning">{pick(zh, '警告', 'Warning')}</option><option value="critical">{pick(zh, '严重（产生告警）', 'Critical (raises alerts)')}</option>
            </select></div>
          <div className="space-y-2"><Label htmlFor="rule-vendor">{pick(zh, '适用厂商', 'Vendor')}</Label>
            <select id="rule-vendor" className={selectClass} value={form.vendor} onChange={(event) => setForm({ ...form, vendor: event.target.value })}>
              <option value="">{pick(zh, '全部厂商', 'All vendors')}</option>{vendors.map((v) => <option key={v} value={v}>{vendorName(v, zh)}</option>)}
            </select></div>
          <div className="space-y-2"><Label htmlFor="rule-group">{pick(zh, '适用分组', 'Group')}</Label>
            <select id="rule-group" className={selectClass} value={form.group_id} onChange={(event) => setForm({ ...form, group_id: event.target.value })}>
              <option value="">{pick(zh, '全部分组', 'All groups')}</option>{inventory?.groups.map((g) => <option key={g.id} value={g.id}>{g.name}</option>)}
            </select></div>
        </div>
        <div className="space-y-2"><Label htmlFor="rule-description">{pick(zh, '说明（可选）', 'Description (optional)')}</Label><Input id="rule-description" value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} /></div>
        <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={form.is_enabled} onChange={(event) => setForm({ ...form, is_enabled: event.target.checked })} />{pick(zh, '启用', 'Enabled')}</label>
        {error ? <p role="alert" className="rounded-md border border-destructive/40 px-3 py-2 text-sm text-destructive">{error}</p> : null}
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
      <Button type="button" onClick={() => setEditing('new')}><Plus aria-hidden="true" />{pick(zh, '新建规则', 'New rule')}</Button>
    </PageHeader>
    <ErrorNote error={rules.error ?? results.error} zh={zh} />
    {summary ? <dl className="grid gap-4 rounded-xl border p-4 sm:grid-cols-4">
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '检查通过率', 'Pass rate')}</dt><dd className="text-lg font-semibold tabular-nums">{rate === null ? '—' : `${rate}%`}</dd></div>
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '未通过的设备', 'Switches failing')}</dt><dd className="text-lg font-semibold tabular-nums">{summary.switches_failing} <span className="text-sm font-normal text-muted-foreground">/ {summary.switches_evaluated}</span></dd></div>
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '未通过的检查项', 'Failed checks')}</dt><dd className="text-lg font-semibold tabular-nums">{summary.failed}</dd></div>
      <div><dt className="text-xs text-muted-foreground">{pick(zh, '通过的检查项', 'Passed checks')}</dt><dd className="text-lg font-semibold tabular-nums">{summary.passed}</dd></div>
    </dl> : null}
    <Section title={pick(zh, '规则', 'Rules')}>
      {rules.data?.length ? <div className="overflow-x-auto rounded-xl border"><Table>
        <TableHeader><TableRow><TableHead>{pick(zh, '名称', 'Name')}</TableHead><TableHead>{pick(zh, '匹配规则', 'Pattern')}</TableHead><TableHead>{pick(zh, '级别', 'Severity')}</TableHead><TableHead>{pick(zh, '范围', 'Scope')}</TableHead><TableHead className="text-right">{pick(zh, '通过 / 未通过', 'Pass / fail')}</TableHead><TableHead /></TableRow></TableHeader>
        <TableBody>{rules.data.map((rule) => <TableRow key={rule.id}>
          <TableCell className="font-medium">{rule.name}{rule.is_enabled ? null : <ToneBadge kind="muted">{pick(zh, '已停用', 'Disabled')}</ToneBadge>}<p className="text-xs font-normal text-muted-foreground">{rule.mode === 'must_contain' ? pick(zh, '必须出现', 'Must be present') : pick(zh, '不得出现', 'Must be absent')}</p></TableCell>
          <TableCell className="max-w-72"><code className="block truncate font-mono text-xs" title={rule.pattern}>{rule.pattern}</code></TableCell>
          <TableCell><SeverityBadge severity={rule.severity} zh={zh} /></TableCell>
          <TableCell className="text-sm">{[rule.vendor ? vendorName(rule.vendor, zh) : null, rule.group_name].filter(Boolean).join(' · ') || pick(zh, '全部', 'All')}</TableCell>
          <TableCell className="text-right tabular-nums">{rule.passed} / <span className={rule.failed ? 'text-destructive' : ''}>{rule.failed}</span></TableCell>
          <TableCell className="text-right"><Button type="button" variant="ghost" size="sm" aria-label={`${pick(zh, '编辑', 'Edit')} ${rule.name}`} onClick={() => setEditing(rule)}><Pencil aria-hidden="true" /></Button></TableCell>
        </TableRow>)}</TableBody>
      </Table></div> : <Empty>{pick(zh, '还没有规则。例如：“必须配置 NTP 服务器”“不得使用 telnet server enable”。', 'No rules yet. For example: an NTP server must be configured, or telnet server enable must be absent.')}</Empty>}
    </Section>
    <Section title={pick(zh, '检查结果', 'Results')} actions={<>
      <select aria-label={pick(zh, '结果', 'Result')} className={cn(selectClass, 'w-auto')} value={status} onChange={(event) => { setStatus(event.target.value); setPage(1) }}>
        <option value="failed">{pick(zh, '仅未通过', 'Failing only')}</option><option value="passed">{pick(zh, '仅通过', 'Passing only')}</option><option value="all">{pick(zh, '全部', 'All')}</option>
      </select>
      <select aria-label={pick(zh, '规则', 'Rule')} className={cn(selectClass, 'w-auto')} value={ruleFilter} onChange={(event) => { setRuleFilter(event.target.value); setPage(1) }}>
        <option value="">{pick(zh, '全部规则', 'All rules')}</option>{rules.data?.map((rule) => <option key={rule.id} value={rule.id}>{rule.name}</option>)}
      </select>
      <Input aria-label={pick(zh, '搜索设备', 'Search switches')} placeholder={pick(zh, '搜索设备', 'Search switches')} value={q} onChange={(event) => { setQ(event.target.value); setPage(1) }} className="w-44" />
    </>}>
      {results.data?.items.length ? <div className="overflow-x-auto rounded-xl border"><Table>
        <TableHeader><TableRow><TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '规则', 'Rule')}</TableHead><TableHead>{pick(zh, '结果', 'Result')}</TableHead><TableHead>{pick(zh, '依据', 'Evidence')}</TableHead><TableHead>{pick(zh, '评估时间', 'Evaluated')}</TableHead></TableRow></TableHeader>
        <TableBody>{results.data.items.map((result) => <TableRow key={`${result.switch_id}:${result.rule_id}`}>
          <TableCell><span className="font-medium">{result.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{result.management_ip}</span></TableCell>
          <TableCell>{result.rule_name} <SeverityBadge severity={result.severity} zh={zh} /></TableCell>
          <TableCell><ToneBadge kind={result.passed ? 'good' : 'bad'}>{result.passed ? pick(zh, '通过', 'Pass') : pick(zh, '未通过', 'Fail')}</ToneBadge></TableCell>
          <TableCell className="max-w-80 break-words font-mono text-xs text-muted-foreground">{result.detail}</TableCell>
          <TableCell><Time value={result.evaluated_at} zh={zh} className="text-xs" /></TableCell>
        </TableRow>)}</TableBody>
      </Table></div> : <Empty>{status === 'failed' ? pick(zh, '没有未通过的检查。', 'Nothing is failing.') : pick(zh, '没有结果。规则需要设备至少有一次配置备份才能评估。', 'No results. Rules are evaluated once a switch has at least one configuration backup.')}</Empty>}
      {results.data ? <Pager page={page} total={results.data.total} pageSize={results.data.page_size} onPage={setPage} zh={zh} /> : null}
    </Section>
    {editing !== null ? <RuleDialog key={editing === 'new' ? 'new' : `${editing.id}:${editing.version}`} rule={editing === 'new' ? null : editing} zh={zh} inventory={inventory.data}
      onClose={() => setEditing(null)} onSaved={() => { setEditing(null); void rules.refetch(); void results.refetch() }} /> : null}
  </main>
}
