import { useQuery } from '@tanstack/react-query'
import { Eye, FileDiff, HardDriveDownload, Wifi } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { vendorName } from '@/features/release/asset-editors'
import { isConfigExcerpt, serverText } from '@/features/release/messages'
import { cn } from '@/lib/utils'
import { opsRequest, type DiffResponse, type SwitchDetail, type TaskKind } from './api'
import { Empty, ErrorNote, ReachabilityBadge, SeverityBadge, ToneBadge, Time, formatTime, itemStatusLabel, kindLabel, pick, workerMessage } from './ui'

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

export function DeviceDetailSheet({ switchId, zh, busy, onClose, onRun }: {
  switchId: string | null; zh: boolean; busy: boolean; onClose: () => void; onRun: (kind: TaskKind, id: string) => void
}) {
  const query = useQuery({ queryKey: ['ops-switch', switchId], queryFn: () => opsRequest<SwitchDetail>(`switches/${switchId}`), enabled: switchId !== null, refetchInterval: 10_000 })
  const [viewing, setViewing] = useState<number | null>(null)
  const [diffing, setDiffing] = useState<{ target: number; base: number } | null>(null)
  const d = query.data
  const itemTone = (status: string) => status === 'succeeded' ? 'good' : status === 'failed' ? 'bad' : 'muted'
  return <Sheet open={switchId !== null} onOpenChange={(open) => { if (!open) onClose() }}>
    <SheetContent className="w-full overflow-y-auto sm:max-w-2xl">
      <SheetHeader>
        <SheetTitle>{d?.name ?? pick(zh, '设备详情', 'Device details')}</SheetTitle>
        <SheetDescription className="font-mono">{d?.management_ip ?? ' '}</SheetDescription>
      </SheetHeader>
      <ErrorNote error={query.error} zh={zh} />
      {d ? <div className="mt-6 flex flex-col gap-8">
        <div className="flex flex-wrap gap-2">
          <Button type="button" size="sm" variant="outline" disabled={busy || !d.is_enabled} onClick={() => onRun('reachability', d.id)}><Wifi aria-hidden="true" data-icon="inline-start" />{pick(zh, '检测连通性', 'Check reachability')}</Button>
          <Button type="button" size="sm" variant="outline" disabled={busy || !d.is_enabled} onClick={() => onRun('config_backup', d.id)}><HardDriveDownload aria-hidden="true" data-icon="inline-start" />{pick(zh, '立即备份', 'Back up now')}</Button>
        </div>
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Info label={pick(zh, '连通性', 'Reachability')}><ReachabilityBadge reachable={d.reachable} latency={d.reachability_latency_ms} error={d.reachability_error} zh={zh} /></Info>
          <Info label={pick(zh, '最近检测', 'Last checked')}><Time value={d.reachability_checked_at} zh={zh} /></Info>
          <Info label={pick(zh, '登录方式', 'Login')}>{d.protocol === 'ssh' ? 'SSH' : 'Telnet'} · {d.port ?? (d.protocol === 'ssh' ? 22 : 23)}</Info>
          <Info label={pick(zh, '厂商 / 型号', 'Vendor / model')}>{vendorName(d.vendor, zh)}{d.model ? ` · ${d.model}` : ''}</Info>
          <Info label={pick(zh, '分组 / 站点', 'Group / site')}>{d.group_name ?? '—'}{d.site ? ` · ${d.site}` : ''}</Info>
          <Info label={pick(zh, '凭据 / 命令模板', 'Credential / profile')}>{d.credential_name} · {d.profile_name}</Info>
          <Info label={pick(zh, '最近备份尝试', 'Last backup attempt')}>{d.config_error ? <span className="text-destructive">{serverText(d.config_error, zh)}</span> : <Time value={d.config_checked_at} zh={zh} />}</Info>
          {d.description ? <Info label={pick(zh, '描述', 'Description')}>{d.description}</Info> : null}
        </dl>
        {d.alerts.length ? <section className="flex flex-col gap-2"><h3 className="text-sm font-semibold">{pick(zh, '未解决告警', 'Open alerts')}</h3>
          <ul className="flex flex-col gap-1.5">{d.alerts.map((alert) => <li key={alert.id} className="flex items-start gap-2 text-sm"><SeverityBadge severity={alert.severity} zh={zh} /><span className="min-w-0 break-words">{serverText(alert.message, zh)}</span></li>)}</ul>
        </section> : null}
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
          <h3 className="text-sm font-semibold">{pick(zh, '最近操作', 'Recent operations')}</h3>
          {d.recent_items.length ? <ul className="divide-y rounded-xl border">{d.recent_items.map((item) => <li key={item.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
            <Time value={item.created_at} zh={zh} className="w-24 shrink-0 text-xs text-muted-foreground" />
            <span className="w-24 shrink-0">{kindLabel(item.kind, zh)}</span>
            <ToneBadge kind={itemTone(item.status)}>{itemStatusLabel(item.status, zh)}</ToneBadge>
            <span className="min-w-0 flex-1 break-words text-xs text-muted-foreground">{workerMessage(item.message, zh)}</span>
          </li>)}</ul> : <Empty>{pick(zh, '还没有对这台设备执行过操作。', 'Nothing has run against this switch yet.')}</Empty>}
        </section>
        {d.networks.length ? <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{pick(zh, '负责的网段', 'Owned networks')}</h3>
          <ul className="flex flex-wrap gap-2">{d.networks.map((network) => <li key={network.id}><ToneBadge kind={network.is_enabled && network.is_validated ? 'good' : 'muted'}><span className="font-mono">{network.cidr}</span>{network.vlan ? ` · VLAN ${network.vlan}` : ''}</ToneBadge></li>)}</ul>
        </section> : null}
      </div> : null}
      <ConfigDialog backupId={viewing} zh={zh} onClose={() => setViewing(null)} />
      <DiffDialog pair={diffing} zh={zh} onClose={() => setDiffing(null)} />
    </SheetContent>
  </Sheet>
}
