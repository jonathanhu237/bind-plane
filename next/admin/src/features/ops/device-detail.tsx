import { useQuery } from '@tanstack/react-query'
import { Download, Eye, FileDiff, HardDriveDownload, PlugZap, RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet'
import { actionName, fieldName, vendorName, type Inventory } from '@/features/release/asset-editors'
import { isConfigExcerpt, serverText } from '@/features/release/messages'
import { cn } from '@/lib/utils'
import { opsRequest, type DiffResponse, type HistoryEvent, type SwitchDetail, type TaskKind } from './api'
import { Empty, ErrorNote, LoginBadge, SeverityBadge, ToneBadge, Time, actorText, formatTime, itemStatusLabel, kindLabel, pick, workerMessage } from './ui'

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

export function DiffDialog({ pair, zh, onClose }: { pair: { target: number; base: number } | null; zh: boolean; onClose: () => void }) {
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
  return Object.entries(event.changes ?? {}).flatMap(([key, value]) => {
    // A network read lists the networks it added and removed.
    if ((key === 'added' || key === 'removed') && Array.isArray(value)) {
      if (!value.length) return []
      const label = key === 'added' ? pick(zh, '新增网段', 'Networks added') : pick(zh, '移除网段', 'Networks removed')
      return `${label}${zh ? '：' : ': '}${value.join(zh ? '、' : ', ')}`
    }
    if (key === 'handles_release') {
      const { to } = (value ?? {}) as { to?: unknown }
      return to ? pick(zh, '设为负责解绑', 'Now handles releases') : pick(zh, '取消负责解绑', 'No longer handles releases')
    }
    const label = key === 'is_enabled' || key === 'is_active' ? pick(zh, '状态', 'state') : fieldName(key, zh)
    if (value === 'replaced' || value === 'cleared') return `${label}${value === 'replaced' ? pick(zh, '已更换', ' replaced') : pick(zh, '已删除', ' removed')}`
    const { from, to } = (value ?? {}) as { from?: unknown; to?: unknown }
    return `${label}${zh ? '：' : ': '}${show(key, from)} → ${show(key, to)}`
  })
}

export function DeviceDetailSheet({ switchId, zh, busy, inventory, onClose, onRun }: {
  switchId: string | null; zh: boolean; busy: boolean; inventory?: Inventory; onClose: () => void; onRun: (kind: TaskKind, id: string) => void
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
          <Button type="button" size="sm" variant="outline" disabled={busy || !d.is_enabled} title={pick(zh, '登录一次再退出，不执行任何命令', 'Signs in and out once; sends no command')} onClick={() => onRun('login_check', d.id)}><PlugZap aria-hidden="true" data-icon="inline-start" />{pick(zh, '测试连接', 'Test connection')}</Button>
          <Button type="button" size="sm" variant="outline" disabled={busy || !d.is_enabled} onClick={() => onRun('config_backup', d.id)}><HardDriveDownload aria-hidden="true" data-icon="inline-start" />{pick(zh, '立即备份', 'Back up now')}</Button>
        </div>
        <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
          <Info label={pick(zh, '连接', 'Connection')}><LoginBadge ok={d.login_ok} error={d.login_error} zh={zh} />
            {d.login_ok === false && d.login_error ? <span className="mt-1 block text-xs text-destructive">{serverText(d.login_error, zh)}</span> : null}</Info>
          <Info label={pick(zh, '最近测试', 'Last tested')}><Time value={d.login_checked_at} zh={zh} /></Info>
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
            <h3 className="text-sm font-semibold">{pick(zh, '网段', 'Networks')}</h3>
            {d.handles_release ? <Button type="button" size="sm" variant="ghost" disabled={busy || !d.is_enabled} title={pick(zh, '登录读取配置，按接口地址更新网段，不修改交换机', 'Signs in, reads the configuration and updates the networks from its interface addresses; changes nothing on the switch')}
              onClick={() => onRun('network_read', d.id)}><RefreshCw aria-hidden="true" data-icon="inline-start" />{pick(zh, '刷新网段', 'Refresh networks')}</Button> : null}
          </div>
          <p className="text-xs text-muted-foreground">{!d.handles_release
            ? pick(zh, '这台交换机不负责解绑，解绑时不会找它。网关交换机请在“编辑”里打开“负责解绑”。', 'This switch does not handle releases, so no release goes to it. Turn on “Handles releases” under Edit for a gateway switch.')
            : d.networks_succeeded_at
              ? <>{pick(zh, '从配置读取于 ', 'Read from its configuration ')}<Time value={d.networks_succeeded_at} zh={zh} />{pick(zh, '，每天自动更新。这些网段里的 IP 解绑时会找到这台交换机。', ', updated every day. Releases of addresses in these networks find this switch.')}</>
              : pick(zh, '还没有读到网段。打开“负责解绑”后会马上读取一次，之后每天自动更新。', 'No networks read yet. They are read right after “Handles releases” is turned on, then every day.')}</p>
          {d.handles_release && d.networks_error ? <p className="text-xs text-destructive">{pick(zh, '最近一次读取失败：', 'Last read failed: ')}{serverText(d.networks_error, zh)}</p> : null}
          {d.networks.length ? <ul className="divide-y rounded-xl border">{d.networks.map((network) => <li key={network.id} className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5 px-3 py-2 text-sm">
            <span className="font-mono">{network.cidr}</span>
            <span className="min-w-0 flex-1 break-words text-xs text-muted-foreground">{[
              network.interface ?? pick(zh, '手工登记，首次读取配置后替换', 'Typed in; replaced by the first read'),
              network.vlan && !network.interface ? `VLAN ${network.vlan}` : '', network.description,
            ].filter(Boolean).join(' · ')}</span>
          </li>)}</ul> : null}
        </section>
        <section className="flex flex-col gap-2">
          <h3 className="text-sm font-semibold">{pick(zh, '配置版本', 'Configuration versions')}</h3>
          {d.backups.length ? <ul className="divide-y rounded-xl border">{d.backups.map((backup, index) => <li key={backup.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 px-3 py-2 text-sm">
            <span className="min-w-0 flex-1"><span className="tabular-nums">{formatTime(backup.captured_at, zh)}</span> <span className="text-xs text-muted-foreground tabular-nums">· {pick(zh, `${backup.line_count} 行`, `${backup.line_count} lines`)}</span></span>
            <Button type="button" variant="ghost" size="sm" onClick={() => setViewing(backup.id)}><Eye aria-hidden="true" data-icon="inline-start" />{pick(zh, '查看', 'View')}</Button>
            <Button asChild variant="ghost" size="sm"><a href={`/api/ops/backups/${backup.id}/download`} download title={pick(zh, '下载这个版本，并记入审计日志', 'Downloads this version; recorded in the audit log')}><Download aria-hidden="true" data-icon="inline-start" />{pick(zh, '下载', 'Download')}</a></Button>
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
                <span className="text-xs text-muted-foreground">{actorText(event.actor_name, event.actor_email, pick(zh, '系统', 'System'))} · <Time value={event.created_at} zh={zh} /></span>
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
    </SheetContent>
  </Sheet>
}
