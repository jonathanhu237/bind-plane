import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { opsRequest, type Alert } from '@/features/ops/api'
import { AdminOnly, Empty, ErrorNote, PageHeader, SeverityBadge, ToneBadge, Time, formatTime, isAdmin, pick, useZh } from '@/features/ops/ui'
import { errorText, serverText } from '@/features/release/messages'
import { notifyRequestError, notifySuccess } from '@/shared/feedback'

export const Route = createFileRoute('/_authenticated/alerts')({ component: AlertsPage })

type AlertState = 'active' | 'resolved'

function AlertsPage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const { t } = useTranslation()
  const admin = isAdmin(user)
  const [status, setStatus] = useState<AlertState>('active')
  const [busy, setBusy] = useState(false)
  const alerts = useQuery({ queryKey: ['ops-alerts', status], queryFn: () => opsRequest<Alert[]>(`alerts?status=${status}`), enabled: admin, refetchInterval: 15_000 })
  async function acknowledge(alert: Alert) {
    setBusy(true)
    try { await opsRequest(`alerts/${alert.id}/acknowledge`, {}); notifySuccess(pick(zh, '已确认。', 'Acknowledged.')); await alerts.refetch() }
    catch (cause) { notifyRequestError(cause, t, { description: errorText(cause, zh) }) }
    finally { setBusy(false) }
  }
  if (!admin) return <AdminOnly zh={zh} />
  const kindName = (kind: Alert['kind']) => ({ device_unreachable: pick(zh, '设备不可达', 'Unreachable'), backup_failed: pick(zh, '备份失败', 'Backup failed'), compliance_violation: pick(zh, '违反合规规则', 'Compliance violation') })[kind]
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-6">
    <PageHeader title={pick(zh, '告警', 'Alerts')} description={pick(zh, '告警在问题消失时自动解决：设备恢复可达、下一次备份成功或配置重新合规。确认表示已有人在处理。', 'Alerts resolve themselves when the cause clears: the switch is reachable again, the next backup succeeds, or the configuration complies. Acknowledging says someone is on it.')} />
    <Tabs value={status} onValueChange={(value) => setStatus(value as AlertState)} className="flex flex-col gap-4">
      <TabsList aria-label={pick(zh, '告警状态', 'Alert state')} className="w-fit">
        <TabsTrigger value="active">{pick(zh, '未解决', 'Active')}</TabsTrigger>
        <TabsTrigger value="resolved">{pick(zh, '已解决', 'Resolved')}</TabsTrigger>
      </TabsList>
      <TabsContent value={status} className="mt-0 flex flex-col gap-4">
        <ErrorNote error={alerts.error} zh={zh} />
        {alerts.data?.length ? <div className="overflow-x-auto rounded-xl border [&_th]:whitespace-nowrap"><Table>
          <TableHeader><TableRow><TableHead>{pick(zh, '级别', 'Severity')}</TableHead><TableHead>{pick(zh, '类型', 'Type')}</TableHead><TableHead>{pick(zh, '设备', 'Switch')}</TableHead><TableHead>{pick(zh, '内容', 'Message')}</TableHead><TableHead>{pick(zh, '首次出现', 'Opened')}</TableHead><TableHead>{status === 'active' ? pick(zh, '处理', 'Handling') : pick(zh, '解决时间', 'Resolved')}</TableHead></TableRow></TableHeader>
          <TableBody>{alerts.data.map((alert) => <TableRow key={alert.id}>
            <TableCell><SeverityBadge severity={alert.severity} zh={zh} /></TableCell>
            <TableCell className="whitespace-nowrap">{kindName(alert.kind)}</TableCell>
            <TableCell className="whitespace-nowrap"><span className="font-medium">{alert.switch_name}</span> <span className="font-mono text-xs text-muted-foreground">{alert.management_ip}</span></TableCell>
            <TableCell className="min-w-64 max-w-96 break-words text-sm">{serverText(alert.message, zh)}<p className="text-xs text-muted-foreground">{pick(zh, '最近一次：', 'Last seen ')}<Time value={alert.last_seen_at} zh={zh} /></p></TableCell>
            <TableCell className="whitespace-nowrap"><Time value={alert.opened_at} zh={zh} className="text-sm" /></TableCell>
            <TableCell className="whitespace-nowrap">{status === 'resolved' ? <Time value={alert.resolved_at} zh={zh} className="text-sm" /> : alert.status === 'acknowledged'
              ? <ToneBadge kind="muted" title={formatTime(alert.acknowledged_at, zh)}>{pick(zh, `${alert.acknowledged_by} 已确认`, `Acknowledged by ${alert.acknowledged_by}`)}</ToneBadge>
              : <Button type="button" variant="outline" size="sm" disabled={busy} onClick={() => void acknowledge(alert)}><Check aria-hidden="true" data-icon="inline-start" />{pick(zh, '确认', 'Acknowledge')}</Button>}</TableCell>
          </TableRow>)}</TableBody>
        </Table></div> : <Empty>{status === 'active' ? pick(zh, '没有未解决的告警。', 'No active alerts.') : pick(zh, '最近 180 天没有已解决的告警。', 'No alerts resolved in the last 180 days.')}</Empty>}
      </TabsContent>
    </Tabs>
  </main>
}
