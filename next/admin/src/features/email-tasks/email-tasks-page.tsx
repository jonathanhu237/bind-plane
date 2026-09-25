import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Eye, RefreshCw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { ApiClient } from '@/shared/api/client'
import type { EmailTask } from '@/shared/api/contracts'
import { isForbidden, translateProblemWithFields } from '@/shared/api/problems'
import { notifyRequestError, notifySuccess, readFailureFeedback, useRequestErrorToast } from '@/shared/feedback'

type Filters = {
  recipient: string
  kind: string
  status: '' | 'queued' | 'sending' | 'waiting_retry' | 'sent' | 'failed'
  from: string
  to: string
  failedOnly: boolean
}

const initialFilters: Filters = { recipient: '', kind: '', status: '', from: '', to: '', failedOnly: false }
const kinds = ['password_reset', 'password_changed', 'user_invitation', 'email_change_code', 'email_changed', 'test_email'] as const
const statuses = ['queued', 'sending', 'waiting_retry', 'sent', 'failed'] as const

export function EmailTasksPage({ api, userID, canWrite = false }: { api: ApiClient; userID: string; canWrite?: boolean }) {
  const { t } = useTranslation(['emailTasks', 'common', 'access'])
  const queryClient = useQueryClient()
  const [filters, setFilters] = useState<Filters>(initialFilters)
  const [cursor, setCursor] = useState<string | undefined>()
  const [history, setHistory] = useState<string[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [detailID, setDetailID] = useState<string | undefined>()
  const [confirm, setConfirm] = useState<{ type: 'delete' | 'bulk-delete'; id?: string }>()
  const rangeError = Boolean(filters.from && filters.to && new Date(filters.from).valueOf() >= new Date(filters.to).valueOf())
  const setFilter = <K extends keyof Filters>(key: K, value: Filters[K]) => {
    setCursor(undefined)
    setHistory([])
    setFilters((current) => ({ ...current, [key]: value }))
  }
  const query = useQuery({
    queryKey: ['email-tasks', userID, filters.recipient, filters.kind, filters.status, filters.from, filters.to, filters.failedOnly, cursor ?? ''],
    queryFn: ({ signal }) => api.getEmailTasks ? api.getEmailTasks({ recipient: filters.recipient || undefined, kind: filters.kind || undefined, status: filters.status || undefined, from: filters.from && !rangeError ? toRFC3339(filters.from) : undefined, to: filters.to && !rangeError ? toRFC3339(filters.to) : undefined, failedOnly: filters.failedOnly || undefined, cursor }, signal) : Promise.reject(new Error('missing getEmailTasks')),
    enabled: !rangeError,
    retry: false,
    refetchInterval: (current) => current.state.error ? false : 3000,
  })
  useRequestErrorToast(query.error, query.isError && !isForbidden(query.error), t, readFailureFeedback(query.error, { unavailableTitle: t('emailTasks:readUnavailableTitle'), unavailableDescription: t('emailTasks:readUnavailableDescription'), forbiddenTitle: t('access:forbiddenTitle'), forbiddenDescription: t('access:forbiddenDescription') }))
  const detail = useQuery({
    queryKey: ['email-task', userID, detailID ?? ''],
    queryFn: ({ signal }) => api.getEmailTask && detailID ? api.getEmailTask(detailID, signal) : Promise.reject(new Error('missing getEmailTask')),
    enabled: Boolean(detailID),
    retry: false,
    refetchInterval: (current) => current.state.error ? false : 3000,
  })
  const retry = useMutation({
    retry: false,
    mutationFn: (id: string) => api.retryEmailTask ? api.retryEmailTask(id) : Promise.reject(new Error('missing retryEmailTask')),
    onSuccess: () => { notifySuccess(t('emailTasks:retried')); void queryClient.invalidateQueries({ queryKey: ['email-tasks', userID] }); void queryClient.invalidateQueries({ queryKey: ['email-task', userID] }) },
    onError: (error) => notifyRequestError(error, t, { title: t('emailTasks:operationFailed'), description: mailTaskErrorDescription(error, t) }),
  })
  const remove = useMutation({
    retry: false,
    mutationFn: (id: string) => api.deleteEmailTask ? api.deleteEmailTask(id) : Promise.reject(new Error('missing deleteEmailTask')),
    onSuccess: () => { setConfirm(undefined); setSelected((items) => items.filter((item) => item !== remove.variables)); notifySuccess(t('emailTasks:deleted')); void queryClient.invalidateQueries({ queryKey: ['email-tasks', userID] }) },
    onError: (error) => notifyRequestError(error, t, { title: t('emailTasks:operationFailed'), description: mailTaskErrorDescription(error, t) }),
  })
  const bulkRetry = useMutation({
    retry: false,
    mutationFn: (ids: string[]) => api.retryEmailTasks ? api.retryEmailTasks(ids) : Promise.reject(new Error('missing retryEmailTasks')),
    onSuccess: (result) => { setSelected([]); notifySuccess(batchResultMessage(result, t)); void queryClient.invalidateQueries({ queryKey: ['email-tasks', userID] }) },
    onError: (error) => notifyRequestError(error, t, { title: t('emailTasks:operationFailed'), description: mailTaskErrorDescription(error, t) }),
  })
  const bulkDelete = useMutation({
    retry: false,
    mutationFn: (ids: string[]) => api.deleteEmailTasks ? api.deleteEmailTasks(ids) : Promise.reject(new Error('missing deleteEmailTasks')),
    onSuccess: (result) => { setConfirm(undefined); setSelected([]); notifySuccess(batchResultMessage(result, t)); void queryClient.invalidateQueries({ queryKey: ['email-tasks', userID] }) },
    onError: (error) => notifyRequestError(error, t, { title: t('emailTasks:operationFailed'), description: mailTaskErrorDescription(error, t) }),
  })
  const tasks = query.data?.tasks ?? []
  const allSelected = tasks.length > 0 && tasks.every((task) => selected.includes(task.id))
  const pending = retry.isPending || remove.isPending || bulkRetry.isPending || bulkDelete.isPending
  return <section className="mx-auto flex w-full max-w-7xl flex-col gap-5" aria-labelledby="email-tasks-title">
    <div><h1 id="email-tasks-title" className="text-2xl font-semibold tracking-tight">{t('emailTasks:title')}</h1></div>
    <Card>
      <CardHeader><CardTitle>{t('emailTasks:filtersTitle')}</CardTitle></CardHeader>
      <CardContent className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="flex flex-col gap-2"><label htmlFor="email-tasks-recipient" className="text-sm font-medium">{t('emailTasks:recipient')}</label><Input id="email-tasks-recipient" placeholder={t('emailTasks:recipientPlaceholder')} value={filters.recipient} onChange={(event) => setFilter('recipient', event.target.value)} /></div>
        <div className="flex flex-col gap-2"><label htmlFor="email-tasks-purpose" className="text-sm font-medium">{t('emailTasks:purpose')}</label><Select value={filters.kind || 'all'} onValueChange={(value) => setFilter('kind', value === 'all' ? '' : value)}><SelectTrigger id="email-tasks-purpose" aria-label={t('emailTasks:purpose')}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">{t('emailTasks:allPurposes')}</SelectItem>{kinds.map((kind) => <SelectItem key={kind} value={kind}>{purposeLabel(kind, t)}</SelectItem>)}</SelectContent></Select></div>
        <div className="flex flex-col gap-2"><label htmlFor="email-tasks-status" className="text-sm font-medium">{t('emailTasks:status')}</label><Select value={filters.status || 'all'} onValueChange={(value) => setFilter('status', value === 'all' ? '' : value as Filters['status'])}><SelectTrigger id="email-tasks-status" aria-label={t('emailTasks:status')}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">{t('emailTasks:allStatuses')}</SelectItem>{statuses.map((status) => <SelectItem key={status} value={status}>{statusLabel(status, t)}</SelectItem>)}</SelectContent></Select></div>
        <div className="flex flex-col gap-2"><label htmlFor="email-tasks-from" className="text-sm font-medium">{t('emailTasks:createdFrom')}</label><Input id="email-tasks-from" type="datetime-local" value={filters.from} onChange={(event) => setFilter('from', event.target.value)} /></div>
        <div className="flex flex-col gap-2"><label htmlFor="email-tasks-to" className="text-sm font-medium">{t('emailTasks:createdTo')}</label><Input id="email-tasks-to" type="datetime-local" value={filters.to} onChange={(event) => setFilter('to', event.target.value)} aria-invalid={rangeError || undefined} />{rangeError ? <p className="text-sm text-destructive">{t('emailTasks:timeRangeError')}</p> : null}</div>
        <label className="flex items-center gap-2 self-end text-sm"><input type="checkbox" checked={filters.failedOnly} onChange={(event) => setFilter('failedOnly', event.target.checked)} />{t('emailTasks:failedOnly')}</label>
        <div className="flex items-end gap-2"><Button type="button" variant="ghost" onClick={() => { setCursor(undefined); setHistory([]); setFilters(initialFilters) }}>{t('emailTasks:resetFilters')}</Button></div>
      </CardContent>
    </Card>
    <Card>
      <CardContent className="pt-6">
        {query.isPending ? <p role="status">{t('emailTasks:loading')}</p> : query.isError && isForbidden(query.error) ? <p role="status">{t('emailTasks:noRead')}</p> : query.isError ? <p role="status">{t('emailTasks:readUnavailableDescription')}</p> : tasks.length === 0 ? <p className="text-sm text-muted-foreground">{t('emailTasks:empty')}</p> : <>
          <div className="mb-4 flex flex-wrap items-center gap-2"><label className="flex items-center gap-2 text-sm"><input type="checkbox" aria-label={t('emailTasks:selectAll')} checked={allSelected} onChange={(event) => setSelected(event.target.checked ? tasks.map((task) => task.id) : [])} />{t('emailTasks:selectAll')}</label>{canWrite ? <><Button size="sm" variant="outline" disabled={pending || selected.length === 0} onClick={() => bulkRetry.mutate(selected)}><RefreshCw aria-hidden="true" data-icon="inline-start" />{t('emailTasks:retrySelected')}</Button><Button size="sm" variant="outline" disabled={pending || selected.length === 0} onClick={() => setConfirm({ type: 'bulk-delete' })}><Trash2 aria-hidden="true" data-icon="inline-start" />{t('emailTasks:deleteSelected')}</Button></> : <span className="text-sm text-muted-foreground">{t('emailTasks:noWrite')}</span>}</div>
          <div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>{t('emailTasks:createdAt')}</TableHead><TableHead>{t('emailTasks:recipient')}</TableHead><TableHead>{t('emailTasks:purpose')}</TableHead><TableHead>{t('emailTasks:language')}</TableHead><TableHead>{t('emailTasks:status')}</TableHead><TableHead>{t('emailTasks:attempts')}</TableHead><TableHead className="text-right">{t('emailTasks:actions')}</TableHead></TableRow></TableHeader><TableBody>{tasks.map((task) => <TableRow key={task.id}><TableCell className="whitespace-nowrap">{formatDate(task.createdAt)}</TableCell><TableCell className="max-w-64 truncate" title={task.recipientEmail}>{task.recipientEmail}</TableCell><TableCell>{purposeLabel(task.kind, t)}</TableCell><TableCell>{task.locale === 'zh-CN' ? t('common:chinese') : t('common:english')}</TableCell><TableCell><Badge variant={task.status === 'failed' ? 'destructive' : task.status === 'sent' ? 'secondary' : 'outline'}>{statusLabel(task.status, t)}</Badge></TableCell><TableCell>{task.attemptCount}</TableCell><TableCell className="text-right"><div className="flex justify-end gap-1"><input type="checkbox" aria-label={`${t('emailTasks:select')} ${task.recipientEmail}`} checked={selected.includes(task.id)} onChange={(event) => setSelected((items) => event.target.checked ? [...items, task.id] : items.filter((item) => item !== task.id))} /> <Button variant="ghost" size="icon" aria-label={t('emailTasks:viewDetails')} onClick={() => setDetailID(task.id)}><Eye aria-hidden="true" /></Button>{canWrite && task.status === 'failed' ? <Button variant="ghost" size="icon" aria-label={t('emailTasks:retry')} disabled={pending} onClick={() => retry.mutate(task.id)}><RefreshCw aria-hidden="true" /></Button> : null}{canWrite ? <Button variant="ghost" size="icon" aria-label={t('emailTasks:delete')} disabled={pending || task.status === 'sending'} onClick={() => setConfirm({ type: 'delete', id: task.id })}><Trash2 aria-hidden="true" /></Button> : null}</div></TableCell></TableRow>)}</TableBody></Table></div>
        </>}
        <div className="mt-4 flex justify-end gap-2"><Button variant="outline" disabled={history.length === 0 || query.isFetching} onClick={() => { const previous = history[history.length - 1]; setHistory((items) => items.slice(0, -1)); setCursor(previous || undefined) }}>{t('access:previousPage')}</Button><Button variant="outline" disabled={!query.data?.nextCursor || query.isFetching} onClick={() => { if (query.data?.nextCursor) { setHistory((items) => [...items, cursor ?? '']); setCursor(query.data.nextCursor) } }}>{t('access:nextPage')}</Button></div>
      </CardContent>
    </Card>
    <Dialog open={Boolean(detailID)} onOpenChange={(open) => { if (!open) setDetailID(undefined) }}><DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t('emailTasks:details')}</DialogTitle><DialogDescription>{t('emailTasks:detailDescription')}</DialogDescription></DialogHeader>{detail.isPending ? <p role="status">{t('common:loading')}</p> : detail.isError ? <p role="status">{t('emailTasks:detailUnavailable')}</p> : detail.data ? <EmailTaskDetail task={detail.data} t={t} /> : null}<DialogFooter><DialogClose asChild><Button variant="outline">{t('common:close')}</Button></DialogClose></DialogFooter></DialogContent></Dialog>
    <Dialog open={Boolean(confirm)} onOpenChange={(open) => { if (!open && !pending) setConfirm(undefined) }}><DialogContent><DialogHeader><DialogTitle>{t('emailTasks:confirmDeleteTitle')}</DialogTitle><DialogDescription>{confirm?.type === 'bulk-delete' ? t('emailTasks:confirmBulkDeleteDescription') : t('emailTasks:confirmDeleteDescription')}</DialogDescription></DialogHeader><DialogFooter><DialogClose asChild><Button variant="outline" disabled={pending}>{t('emailTasks:cancel')}</Button></DialogClose><Button variant="destructive" disabled={pending} onClick={() => { if (confirm?.type === 'bulk-delete') bulkDelete.mutate(selected); else if (confirm?.id) remove.mutate(confirm.id) }}>{t('emailTasks:delete')}</Button></DialogFooter></DialogContent></Dialog>
  </section>
}

function mailTaskErrorDescription(error: unknown, t: (key: any, options?: any) => string): string { return isForbidden(error) ? t('access:forbiddenDescription') : translateProblemWithFields(error, t) }
function EmailTaskDetail({ task, t }: { task: EmailTask; t: (key: any, options?: any) => string }) {
  return <div className="flex min-w-0 flex-col gap-6 text-sm"><div className="flex flex-wrap items-center gap-3"><p className="text-xl font-semibold">{purposeLabel(task.kind, t)}</p><Badge variant={task.status === 'failed' ? 'destructive' : 'secondary'}>{statusLabel(task.status, t)}</Badge></div><dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2"><Detail label={t('emailTasks:recipient')} value={task.recipientEmail} /><Detail label={t('emailTasks:language')} value={task.locale === 'zh-CN' ? t('common:chinese') : t('common:english')} /><Detail label={t('emailTasks:createdAt')} value={formatDate(task.createdAt)} /><Detail label={t('emailTasks:attempts')} value={String(task.attemptCount)} />{task.finishedAt ? <Detail label={t('emailTasks:finishedAt')} value={formatDate(task.finishedAt)} /> : null}</dl>{task.lastErrorCode ? <p className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-destructive">{failureLabel(task.lastErrorCode, t)}</p> : null}<section><h3 className="mb-3 font-semibold">{t('emailTasks:attemptHistory')}</h3>{task.attempts?.length ? <ul className="space-y-2">{task.attempts.map((attempt, index) => <li key={attempt.id ?? `${attempt.occurredAt}-${index}`} className="flex flex-wrap justify-between gap-3 rounded-md border p-3"><span>{formatDate(attempt.occurredAt)}</span><span>{attempt.outcome === 'sent' ? t('emailTasks:outcomeSent') : failureLabel(attempt.errorCode ?? 'permanent', t)}</span></li>)}</ul> : <p className="text-muted-foreground">{t('emailTasks:noAttempts')}</p>}</section></div>
}
function Detail({ label, value }: { label: string; value: string }) { return <div className="min-w-0"><dt className="text-muted-foreground">{label}</dt><dd className="mt-1 break-words font-medium">{value}</dd></div> }
function purposeLabel(kind: string, t: (key: any, options?: any) => string): string { const keys: Record<string, string> = { password_reset: 'purposePasswordReset', password_changed: 'purposePasswordChanged', user_invitation: 'purposeInvitation', email_change_code: 'purposeEmailChangeCode', email_changed: 'purposeEmailChanged', test_email: 'purposeTestEmail' }; return t(`emailTasks:${keys[kind] ?? kind}`) }
function statusLabel(status: string, t: (key: any, options?: any) => string): string { const keys: Record<string, string> = { queued: 'queued', sending: 'sending', waiting_retry: 'waitingRetry', sent: 'sent', failed: 'failed' }; return t(`emailTasks:${keys[status] ?? status}`) }
function failureLabel(code: string, t: (key: any, options?: any) => string): string { const keys: Record<string, string> = { temporary: 'failureTemporary', permanent: 'failurePermanent', dependency: 'failureDependency', expired: 'failureExpired', superseded: 'failureSuperseded', invalid_reset: 'failureInvalid', invalid_invitation: 'failureInvalid', invalid_email_change: 'failureInvalid' }; return t(`emailTasks:${keys[code] ?? 'failurePermanent'}`) }
function batchResultMessage(result: { succeeded: number; skipped: number; failed: number; items: Array<{ result: 'succeeded' | 'skipped' | 'failed'; code?: string }> }, t: (key: any, options?: any) => string): string {
  const reasons = Array.from(new Set(result.items.filter((item) => item.result !== 'succeeded' && item.code).map((item) => batchSkipReason(item.code!, t))))
  const summary = t('emailTasks:partialResult', result)
  return reasons.length > 0 ? `${summary} ${t('emailTasks:skippedReasons', { reasons: reasons.join(', ') })}` : summary
}
function batchSkipReason(code: string, t: (key: any, options?: any) => string): string {
  const keys: Record<string, string> = { sending: 'sending', not_failed: 'notFailed', not_found: 'notFound', dependency_unavailable: 'failureDependency', operation_failed: 'operationFailed' }
  return t(`emailTasks:${keys[code] ?? 'operationFailed'}`)
}
function toRFC3339(value: string): string { return new Date(value).toISOString() }
function formatDate(value: string): string { const date = new Date(value); return Number.isNaN(date.valueOf()) ? value : date.toLocaleString() }
