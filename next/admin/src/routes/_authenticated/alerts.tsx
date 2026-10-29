import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { opsRequest, type Alert } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, SeverityBadge, ToneBadge, Time, isAdmin, pick, useZh } from '@/features/ops/ui'
import { assetErrorText } from '@/features/release/asset-editors'

export const Route = createFileRoute('/_authenticated/alerts')({ component: AlertsPage })

function AlertsPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
  const [status, setStatus] = useState<'active' | 'resolved'>('active')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const alerts = useQuery({ queryKey: ['ops-alerts', status], queryFn: () => opsRequest<Alert[]>(`alerts?status=${status}`), enabled: admin, refetchInterval: 15_000 })
  async function acknowledge(alert: Alert) {
    setBusy(true); setMessage('')
    try { await opsRequest(`alerts/${alert.id}/acknowledge`, {}); await alerts.refetch() }
    catch (cause) { setMessage(assetErrorText(cause, zh)) }
    finally { setBusy(false) }
  }
  if (!admin) return <AdminOnly zh={zh} />
  const kindName = (kind: Alert['kind']) => ({ device_unreachable: pick(zh, '设备不可达', 'Unreachable'), backup_failed: pick(zh, '备份失败', 'Backup failed'), compliance_violation: pick(zh, '违反合规规则', 'Compliance violation') })[kind]
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '告警', 'Alerts')} description={pick(zh, '告警在问题消失时自动解决：设备恢复可达、下一次备份成功或配置重新合规。确认表示已有人在处理。', 'Alerts resolve themselves when the cause clears: the switch is reachable again, the next backup succeeds, or the configuration complies. Acknowledging says someone is on it.')} />
    <div role="tablist" aria-label={pick(zh, '告警状态', 'Alert state')} className="inline-flex w-fit rounded-md border p-0.5">
      {(['active', 'resolved'] as const).map((value) => <button key={value} type="button" role="tab" aria-selected={status === value} onClick={() => setStatus(value)}
        className={`rounded px-3 py-1.5 text-sm ${status === value ? 'bg-muted font-medium' : 'text-muted-foreground hover:text-foreground'}`}>{value === 'active' ? pick(zh, '未解决', 'Active') : pick(zh, '已解决', 'Resolved')}</button>)}
    </div>
    {message ? <p role="status" className="rounded-md border px-3 py-2 text-sm">{message}</p> : null}
    <ErrorNote error={alerts.error} zh={zh} />
    {alerts.data?.length ? <div className="overflow-x-auto rounded-xl border"><Table>
      <TableHeader><TableRow><TableHead>{pick(zh, '级别', 'Severity')}</TableHead><TableHead>{pick(zh, '类型', 'Type')}</TableHead><TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '内容', 'Message')}</TableHead><TableHead>{pick(zh, '首次出现', 'Opened')}</TableHead><TableHead>{status === 'active' ? pick(zh, '处理', 'Handling') : pick(zh, '解决时间', 'Resolved')}</TableHead></TableRow></TableHeader>
      <TableBody>{alerts.data.map((alert) => <TableRow key={alert.id}>
        <TableCell><SeverityBadge severity={alert.severity} zh={zh} /></TableCell>
        <TableCell className="whitespace-nowrap">{kindName(alert.kind)}</TableCell>
        <TableCell className="whitespace-nowrap"><span className="font-medium">{alert.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{alert.management_ip}</span></TableCell>
        <TableCell className="max-w-96 break-words text-sm">{alert.message}<p className="text-xs text-muted-foreground">{pick(zh, '最近一次：', 'Last seen ')}<Time value={alert.last_seen_at} zh={zh} /></p></TableCell>
        <TableCell><Time value={alert.opened_at} zh={zh} className="text-sm" /></TableCell>
        <TableCell>{status === 'resolved' ? <Time value={alert.resolved_at} zh={zh} className="text-sm" /> : alert.status === 'acknowledged'
          ? <ToneBadge kind="muted" title={alert.acknowledged_at ?? undefined}>{pick(zh, `${alert.acknowledged_by} 已确认`, `Acknowledged by ${alert.acknowledged_by}`)}</ToneBadge>
          : <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void acknowledge(alert)}><Check aria-hidden="true" />{pick(zh, '确认', 'Acknowledge')}</Button>}</TableCell>
      </TableRow>)}</TableBody>
    </Table></div> : <Empty>{status === 'active' ? pick(zh, '没有未解决的告警。', 'No active alerts.') : pick(zh, '最近 180 天没有已解决的告警。', 'No alerts resolved in the last 180 days.')}</Empty>}
  </main>
}
