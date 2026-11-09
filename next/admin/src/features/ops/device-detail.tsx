import { useQuery } from '@tanstack/react-query'
import { CircleAlert, Eye, FileDiff, HardDriveDownload, Plus, PlugZap } from 'lucide-react'
import { useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import {
  AssetEditDialog, AssetRequestError, AssetRow, DisableAssetDialog, UnvalidatedBadge, actionName, callAssets, editorLabels, fieldName, statusBody, vendorName,
  type EditTarget, type Inventory,
} from '@/features/release/asset-editors'
import { errorText, isConfigExcerpt, serverText } from '@/features/release/messages'
import { cn } from '@/lib/utils'
import { notifySuccess } from '@/shared/feedback'
import { opsRequest, type DiffResponse, type HistoryEvent, type SwitchDetail, type TaskKind } from './api'
import { Empty, ErrorNote, LoginBadge, ReachabilityBadge, SeverityBadge, ToneBadge, Time, actorText, formatTime, itemStatusLabel, kindLabel, pick, workerMessage } from './ui'

function Info({ label, children }: { label: string; children: React.ReactNode }) {
  return <div className="min-w-0"><dt className="text-xs text-muted-foreground">{label}</dt><dd className="mt-0.5 break-words text-sm">{children}</dd></div>
}

function ConfigDialog({ backupId, zh, onClose }: { backupId: number | null; zh: boolean; onClose: () => void }) {
  const query = useQuery({ queryKey: ['ops-backup', backupId], queryFn: () => opsRequest<{ switch_name: string; captured_at: string; content: string }>(`backups/${backupId}`), enabled: backupId !== null, staleTime: Infinity })
  const lines = query.data?.content.split('\n') ?? []
  return <Dialog open={backupId !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="flex max-h-[90dvh] flex-col sm:max-w-4xl">
      <DialogHeader>
        <DialogTitle>{query.data ? `${query.data.switch_name} · ${formatTime(query.data.captured_at, zh)}` : pick(zh, '配置内容', 'Configuration')}</DialogTitle>
        <DialogDescription>{pick(zh, '配置中可能含有密码哈希和团体字，本次查看已记入审计日志。', 'Configurations can hold password hashes and SNMP communities. This view is recorded in the audit log.')}</DialogDescription>
      </DialogHeader>
      <ErrorNote error={query.error} zh={zh} />
      {query.data ? <div className="min-h-0 overflow-auto rounded-md border bg-muted/30">
        <pre className="font-mono text-xs leading-5"><code>{lines.map((line, index) => <div key={index} className="flex"><span className="w-12 shrink-0 select-none border-r px-2 text-right text-muted-foreground tabular-nums">{index + 1}</span><span className="whitespace-pre px-3">{line || ' '}</span></div>)}</code></pre>
      </div> : null}
    </DialogContent>
  </Dialog>
}

function DiffDialog({ pair, zh, onClose }: { pair: { target: number; base: number } | null; zh: boolean; onClose: () => void }) {
  const query = useQuery({ queryKey: ['ops-diff', pair?.target, pair?.base], queryFn: () => opsRequest<DiffResponse>(`backups/${pair?.target}/diff?base=${pair?.base}`), enabled: pair !== null, staleTime: Infinity })
  const data = query.data
  return <Dialog open={pair !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <DialogContent closeLabel={pick(zh, '关闭', 'Close')} className="flex max-h-[90dvh] flex-col sm:max-w-4xl">
      <DialogHeader>
        <DialogTitle>{data ? pick(zh, `${data.switch_name} 配置变更`, `${data.switch_name} configuration changes`) : pick(zh, '配置对比', 'Configuration diff')}</DialogTitle>
        <DialogDescription>{data ? `${formatTime(data.base.captured_at, zh)} → ${formatTime(data.target.captured_at, zh)}` : ' '}</DialogDescription>
      </DialogHeader>
      <ErrorNote error={query.error} zh={zh} />
      {data ? <>
        <p className="text-sm"><span className="font-medium text-success tabular-nums">+{data.diff.added}</span> <span className="ml-2 font-medium text-destructive tabular-nums">−{data.diff.removed}</span>
          {data.diff.replaced ? <span className="ml-3 text-muted-foreground">{pick(zh, '两个版本差异过大，按整体替换显示。', 'The versions differ too much to align, so this shows a full replacement.')}</span> : null}</p>
        <div className="min-h-0 overflow-auto rounded-md border font-mono text-xs leading-5">
          {data.diff.hunks.length ? data.diff.hunks.map((hunk, index) => <div key={index} className="border-b last:border-b-0">
            <div className="bg-muted/50 px-3 py-1 text-muted-foreground">@@ −{hunk.old_start},{hunk.old_lines} +{hunk.new_start},{hunk.new_lines} @@</div>
            {hunk.lines.map((line, i) => <div key={i} className={cn('whitespace-pre px-3', line.kind === 'add' && 'bg-success/10 text-success', line.kind === 'remove' && 'bg-destructive/10 text-destructive')}>
              <span className="select-none pr-2">{line.kind === 'add' ? '+' : line.kind === 'remove' ? '−' : ' '}</span>{line.text || ' '}
            </div>)}
          </div>) : <p className="p-3 text-muted-foreground">{pick(zh, '两个版本没有差异。', 'The two versions are identical.')}</p>}
        </div>
      </> : null}
    </DialogContent>
  </Dialog>
}

/** One audit record as "field: before → after", with IDs shown by the names they have now. */
function changeLines(event: HistoryEvent, inventory: Inventory | undefined, zh: boolean): string[] {
  const named: Record<string, { id: string; name: string }[] | undefined> = {
    group_id: inventory?.groups, credential_id: inventory?.credentials, command_profile_id: inventory?.profiles, switch_id: inventory?.switches,
  }
  const show = (key: string, value: unknown): string => {
    if (value === null || value === undefined || value === '') return pick(zh, '（空）', '(empty)')
    if (typeof value === 'boolean') {
      if (key === 'is_validated') return value ? pick(zh, '已验证', 'validated') : pick(zh, '未验证', 'unvalidated')
      return value ? pick(zh, '启用', 'enabled') : pick(zh, '停用', 'disabled')
    }
    const list = named[key]
    if (list && typeof value === 'string') return list.find((item) => item.id === value)?.name ?? pick(zh, '（已删除）', '(deleted)')
    return String(value)
  }
  return Object.entries(event.changes ?? {}).map(([key, value]) => {
    const label = key === 'is_enabled' || key === 'is_active' ? pick(zh, '状态', 'state') : fieldName(key, zh)
    if (value === 'replaced' || value === 'cleared') return `${label}${value === 'replaced' ? pick(zh, '已更换', ' replaced') : pick(zh, '已删除', ' removed')}`
    const { from, to } = (value ?? {}) as { from?: unknown; to?: unknown }
    return `${label}${zh ? '：' : ': '}${show(key, from)} → ${show(key, to)}`
  })
}

function NetworkForm({ switchId, zh, onAdded }: { switchId: string; zh: boolean; onAdded: () => Promise<void> }) {
  const empty = { cidr: '', vlan: '', description: '', is_validated: false }
  const [form, setForm] = useState(empty)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  async function submit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!form.cidr.trim()) { setError(pick(zh, '请输入网段，例如 10.0.0.0/24。', 'Enter a network, such as 10.0.0.0/24.')); return }
    setBusy(true); setError('')
    try { await callAssets('networks', { switch_id: switchId, ...form, cidr: form.cidr.trim() }); setForm(empty); notifySuccess(pick(zh, '网段已添加。', 'Network added.')); await onAdded() }
    catch (cause) { setError(errorText(cause, zh)) }
    finally { setBusy(false) }
  }
  return <form className="flex flex-col gap-3 rounded-xl border p-3" noValidate onSubmit={(event) => void submit(event)}>
    <FieldGroup className="gap-3">
      <div className="grid gap-3 sm:grid-cols-[2fr_1fr]">
        <Field data-invalid={Boolean(error) && !form.cidr.trim()}>
          <FieldLabel htmlFor="network-new-cidr">IPv4 CIDR</FieldLabel>
          <Input id="network-new-cidr" className="font-mono" placeholder="10.0.0.0/24" value={form.cidr} onChange={(event) => setForm({ ...form, cidr: event.target.value })} autoComplete="off" />
        </Field>
        <Field>
          <FieldLabel htmlFor="network-new-vlan">{pick(zh, 'VLAN（可选）', 'VLAN (optional)')}</FieldLabel>
          <Input id="network-new-vlan" value={form.vlan} onChange={(event) => setForm({ ...form, vlan: event.target.value })} autoComplete="off" />
        </Field>
      </div>
      <Field>
        <FieldLabel htmlFor="network-new-description">{pick(zh, '描述（可选）', 'Description (optional)')}</FieldLabel>
        <Input id="network-new-description" value={form.description} onChange={(event) => setForm({ ...form, description: event.target.value })} autoComplete="off" />
      </Field>
      <Field orientation="horizontal">
        <Checkbox id="network-new-validated" checked={form.is_validated} onCheckedChange={(checked) => setForm({ ...form, is_validated: checked === true })} />
        <FieldLabel htmlFor="network-new-validated" className="font-normal">{pick(zh, '已人工确认这个网段归这台交换机', 'Ownership confirmed by hand')}</FieldLabel>
      </Field>
    </FieldGroup>
    {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
    <Button type="submit" size="sm" className="self-start" disabled={busy}>{busy ? pick(zh, '正在保存…', 'Saving…') : pick(zh, '添加网段', 'Add network')}</Button>
  </form>
}

export function DeviceDetailSheet({ switchId, zh, busy, inventory, onClose, onRun, onChanged }: {
  switchId: string | null; zh: boolean; busy: boolean; inventory?: Inventory; onClose: () => void; onRun: (kind: TaskKind, id: string) => void; onChanged: () => Promise<void>
}) {
  const query = useQuery({ queryKey: ['ops-switch', switchId], queryFn: () => opsRequest<SwitchDetail>(`switches/${switchId}`), enabled: switchId !== null, refetchInterval: 10_000 })
  const [viewing, setViewing] = useState<number | null>(null)
  const [diffing, setDiffing] = useState<{ target: number; base: number } | null>(null)
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<EditTarget | null>(null)
  const [disabling, setDisabling] = useState<EditTarget | null>(null)
  const [working, setWorking] = useState(false)
  const [error, setError] = useState('')
  const labels = editorLabels(zh)
  const d = query.data
  const itemTone = (status: string) => status === 'succeeded' ? 'good' : status === 'failed' ? 'bad' : 'muted'
  const usable = d?.networks.filter((network) => network.is_enabled && network.is_validated).length ?? 0

  async function changed() { await Promise.all([query.refetch(), onChanged()]) }
  async function saveEdit(path: string, body: Record<string, unknown>) {
    try { await callAssets(path, body, 'PATCH') }
    catch (cause) { if (cause instanceof AssetRequestError && cause.status === 409) await changed(); throw cause }
    setEditing(null); notifySuccess(labels.saved); await changed()
  }
  async function setEnabled(target: EditTarget, on: boolean) {
    setWorking(true); setError('')
    try { await callAssets(`${target.kind}/${target.item.id}`, statusBody(target, on), 'PATCH'); notifySuccess(labels.statusChanged(target, on)) }
    catch (cause) { setError(errorText(cause, zh)) }
    finally { await changed(); setWorking(false) }
  }

  return <Sheet open={switchId !== null} onOpenChange={(open) => { if (!open) { setAdding(false); setError(''); onClose() } }}>
    <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
      <SheetHeader>
        <SheetTitle>{d?.name ?? pick(zh, '设备详情', 'Device details')}</SheetTitle>
        <SheetDescription className="font-mono">{d?.management_ip ?? ' '}</SheetDescription>
      </SheetHeader>
      <ErrorNote error={query.error} zh={zh} />
      {d ? <div className="mt-6 flex flex-col gap-8">
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" variant="outline" disabled={busy || !d.is_enabled} title={pick(zh, '登录一次再退出，不执行任何命令', 'Signs in and out once; sends no command')} onClick={() => onRun('login_check', d.id)}><PlugZap aria-hidden="true" data-icon="inline-start" />{pick(zh, '测试连接', 'Test connection')}</Button>
          <Button type="button" size="sm" variant="outline" disabled={busy || !d.is_enabled} onClick={() => onRun('config_backup', d.id)}><HardDriveDownload aria-hidden="true" data-icon="inline-start" />{pick(zh, '立即备份', 'Back up now')}</Button>
        </div>
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Info label={pick(zh, '连接', 'Connection')}><span className="flex flex-wrap gap-1"><ReachabilityBadge reachable={d.reachable} latency={d.reachability_latency_ms} error={d.reachability_error} zh={zh} /><LoginBadge reachable={d.reachable} ok={d.login_ok} error={d.login_error} zh={zh} /></span>
            {d.reachable && d.login_ok === false && d.login_error ? <span className="mt-1 block text-xs text-destructive">{serverText(d.login_error, zh)}</span> : null}</Info>
          <Info label={pick(zh, '最近检测', 'Last checked')}><Time value={d.reachability_checked_at} zh={zh} /></Info>
          <Info label={pick(zh, '登录方式', 'Login')}>{d.protocol === 'ssh' ? 'SSH' : 'Telnet'} · {d.port ?? (d.protocol === 'ssh' ? 22 : 23)}</Info>
          <Info label={pick(zh, '厂商 / 型号', 'Vendor / model')}>{vendorName(d.vendor, zh)}{d.model ? ` · ${d.model}` : ''}</Info>
          <Info label={pick(zh, '分组 / 校区', 'Group / campus')}>{d.group_name ?? '—'}{d.site ? ` · ${d.site}` : ''}</Info>
          <Info label={pick(zh, '凭据 / 命令模板', 'Credential / profile')}>{d.credential_name} · {d.profile_name}</Info>
          <Info label={pick(zh, '最近成功备份', 'Last successful backup')}><Time value={d.config_succeeded_at} zh={zh} />
            {d.config_error ? <span className="mt-1 block text-xs text-destructive">{pick(zh, '最近一次失败：', 'Last attempt failed: ')}{serverText(d.config_error, zh)}</span> : null}</Info>
          {d.description ? <Info label={pick(zh, '描述', 'Description')}>{d.description}</Info> : null}
        </dl>
        {d.alerts.length ? <section className="flex flex-col gap-2"><h3 className="text-sm font-semibold">{pick(zh, '未解决告警', 'Open alerts')}</h3>
          <ul className="flex flex-col gap-1.5">{d.alerts.map((alert) => <li key={alert.id} className="flex items-start gap-2 text-sm"><SeverityBadge severity={alert.severity} zh={zh} /><span className="min-w-0 break-words">{serverText(alert.message, zh)}</span></li>)}</ul>
        </section> : null}
        <section className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <h3 className="text-sm font-semibold">{pick(zh, '负责的网段', 'Owned networks')}</h3>
            {adding ? null : <Button type="button" size="sm" variant="ghost" onClick={() => setAdding(true)}><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '添加网段', 'Add network')}</Button>}
          </div>
          <p className="text-xs text-muted-foreground">{usable
            ? pick(zh, '这些网段里的 IP 解绑时会找到这台交换机。只有已启用、已验证的网段参与查找。', 'Releases of addresses in these networks find this switch. Only enabled, validated networks count.')
            : pick(zh, '还没有可用的网段：这台交换机下的 IP 解绑时会提示“未匹配到交换机”。添加网段并确认归属后才能解绑。', 'No usable network yet: releases of its addresses stop with “No switch matched” until a network is added and confirmed.')}</p>
          {adding ? <NetworkForm switchId={d.id} zh={zh} onAdded={async () => { setAdding(false); await changed() }} /> : null}
          {error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null}
          {d.networks.length ? <ul className="divide-y rounded-xl border px-3">{d.networks.map((network) => {
            const target: EditTarget = { kind: 'networks', item: network }
            return <AssetRow key={network.id} title={network.cidr} mono on={network.is_enabled} busy={working} labels={labels} onEdit={() => setEditing(target)}
              onToggle={() => network.is_enabled ? setDisabling(target) : void setEnabled(target, true)}
              flag={network.is_validated ? null : <UnvalidatedBadge labels={labels} />}
              detail={[network.vlan ? `VLAN ${network.vlan}` : '', network.description].filter(Boolean).join(' · ') || undefined} />
          })}</ul> : null}
        </section>
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{pick(zh, '配置版本', 'Configuration versions')}</h3>
          {d.backups.length ? <ul className="divide-y rounded-xl border">{d.backups.map((backup, index) => <li key={backup.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
            <span className="min-w-0 flex-1"><span className="tabular-nums">{formatTime(backup.captured_at, zh)}</span> <span className="text-xs text-muted-foreground tabular-nums">· {pick(zh, `${backup.line_count} 行`, `${backup.line_count} lines`)}</span></span>
            <Button type="button" variant="ghost" size="sm" onClick={() => setViewing(backup.id)}><Eye aria-hidden="true" data-icon="inline-start" />{pick(zh, '查看', 'View')}</Button>
            {d.backups[index + 1] ? <Button type="button" variant="ghost" size="sm" onClick={() => setDiffing({ target: backup.id, base: d.backups[index + 1].id })}><FileDiff aria-hidden="true" data-icon="inline-start" />{pick(zh, '与上一版对比', 'Diff with previous')}</Button> : null}
          </li>)}</ul> : <Empty>{pick(zh, '还没有配置备份。只有配置发生变化时才会保存新版本。', 'No backups yet. A new version is stored only when the configuration changes.')}</Empty>}
        </section>
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{pick(zh, '合规检查', 'Compliance')}</h3>
          {d.compliance.length ? <ul className="divide-y rounded-xl border">{d.compliance.map((result) => {
            const evidence = serverText(result.detail, zh)
            return <li key={result.rule_id} className="flex flex-col gap-1 px-3 py-2 text-sm">
              <div className="flex flex-wrap items-center gap-2"><ToneBadge kind={result.passed ? 'good' : 'bad'}>{result.passed ? pick(zh, '通过', 'Pass') : pick(zh, '未通过', 'Fail')}</ToneBadge><span className="font-medium">{result.rule_name}</span><SeverityBadge severity={result.severity} zh={zh} /></div>
              {evidence ? <p className={cn('break-words text-xs text-muted-foreground', isConfigExcerpt(result.detail) && 'font-mono')}>{evidence}</p> : null}
            </li>
          })}</ul> : <Empty>{pick(zh, '没有适用的规则或尚未评估。', 'No rule applies yet, or none has been evaluated.')}</Empty>}
        </section>
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{pick(zh, '变更历史', 'Change history')}</h3>
          {d.history.length ? <ul className="divide-y rounded-xl border">{d.history.map((event) => {
            const lines = changeLines(event, inventory, zh)
            return <li key={event.id} className="flex flex-col gap-1 px-3 py-2 text-sm">
              <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <span className="font-medium">{actionName(event.action, zh)}{event.action.startsWith('network.') ? <span className="ml-1 font-mono font-normal">{event.object_name}</span> : null}</span>
                <span className="text-xs text-muted-foreground">{actorText(event.actor_name, event.actor_email)} · <Time value={event.created_at} zh={zh} /></span>
              </div>
              {lines.length ? <ul className="flex flex-col gap-0.5 text-xs text-muted-foreground">{lines.map((line) => <li key={line} className="break-words">{line}</li>)}</ul> : null}
            </li>
          })}</ul> : <Empty>{pick(zh, '还没有变更记录。', 'No changes recorded yet.')}</Empty>}
        </section>
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{pick(zh, '最近操作', 'Recent operations')}</h3>
          {d.recent_items.length ? <ul className="divide-y rounded-xl border">{d.recent_items.slice(0, 10).map((item) => <li key={item.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
            <Time value={item.created_at} zh={zh} className="w-24 shrink-0 text-xs text-muted-foreground" />
            <span className="w-24 shrink-0">{kindLabel(item.kind, zh)}</span>
            <ToneBadge kind={itemTone(item.status)}>{itemStatusLabel(item.status, zh)}</ToneBadge>
            <span className="min-w-0 flex-1 break-words text-xs text-muted-foreground">{workerMessage(item.message, zh)}</span>
          </li>)}</ul> : <Empty>{pick(zh, '还没有对这台设备执行过操作。', 'Nothing has run against this switch yet.')}</Empty>}
        </section>
      </div> : null}
      <ConfigDialog backupId={viewing} zh={zh} onClose={() => setViewing(null)} />
      <DiffDialog pair={diffing} zh={zh} onClose={() => setDiffing(null)} />
      <AssetEditDialog target={editing} inventory={inventory} zh={zh} onClose={() => setEditing(null)} onSave={saveEdit} />
      <DisableAssetDialog target={disabling} inventory={inventory} zh={zh} onCancel={() => setDisabling(null)} onConfirm={(target) => { setDisabling(null); void setEnabled(target, false) }} />
    </SheetContent>
  </Sheet>
}
