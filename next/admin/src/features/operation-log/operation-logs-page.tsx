import { useQuery } from '@tanstack/react-query'
import { Eye } from 'lucide-react'
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
import type { OperationLog } from '@/shared/api/contracts'
import { isForbidden } from '@/shared/api/problems'
import { readFailureFeedback, useRequestErrorToast } from '@/shared/feedback'

type Translator = (key: any) => string

type Filters = {
  action: string
  objectType: string
  objectId: string
  actorId: string
  from: string
  to: string
  result: '' | 'success' | 'failure'
}

const knownActions = [
  'auth.login', 'auth.logout', 'auth.password_reset.request', 'auth.password_reset.complete', 'auth.invitation.accept', 'auth.setup.complete',
  'roles.create', 'roles.update', 'roles.delete', 'users.roles.update', 'users.sessions.revoke', 'users.deactivate', 'users.reactivate', 'users.delete', 'invitations.create', 'invitations.resend', 'invitations.revoke',
  'settings.email.update', 'settings.email.test', 'settings.operation_log_retention.update',
  'mail_tasks.retry', 'mail_tasks.delete', 'mail_tasks.bulk_retry', 'mail_tasks.bulk_delete',
  'auth.profile.name.update', 'auth.profile.avatar.update', 'auth.profile.avatar.delete', 'auth.profile.password.update', 'auth.profile.email.request', 'auth.profile.email.resend', 'auth.profile.email.update',
]
const knownObjectTypes = ['session', 'account', 'password_reset', 'password', 'invitation', 'email_change', 'role', 'user', 'email_settings', 'operation_log_settings', 'mail_task']
const initialFilters: Filters = { action: '', objectType: '', objectId: '', actorId: '', from: '', to: '', result: '' }

export function OperationLogsPage({ api, userID }: { api: ApiClient; userID: string }) {
  const { t } = useTranslation(['operationLog', 'common', 'access'])
  const [filters, setFilters] = useState<Filters>(initialFilters)
  const [cursor, setCursor] = useState<string | undefined>()
  const [history, setHistory] = useState<string[]>([])
  const [selectedID, setSelectedID] = useState<string | undefined>()
  const rangeError = Boolean(filters.from && filters.to && new Date(filters.from).valueOf() >= new Date(filters.to).valueOf())
  const actorError = Boolean(filters.actorId && !isUUID(filters.actorId))
  const setFilter = <K extends keyof Filters>(key: K, value: Filters[K]) => {
    setCursor(undefined)
    setHistory([])
    setFilters((current) => ({ ...current, [key]: value }))
  }
  const query = useQuery({
    queryKey: ['operation-logs', userID, filters.action, filters.objectType, filters.objectId, filters.actorId, filters.from, filters.to, filters.result, cursor ?? ''],
    queryFn: ({ signal }) => api.getOperationLogs ? api.getOperationLogs({
      action: filters.action || undefined,
      objectType: filters.objectType || undefined,
      objectId: filters.objectId || undefined,
      actorId: filters.actorId || undefined,
      from: filters.from && !rangeError ? toRFC3339(filters.from) : undefined,
      to: filters.to && !rangeError ? toRFC3339(filters.to) : undefined,
      result: filters.result || undefined,
      cursor,
    }, signal) : Promise.reject(new Error('missing getOperationLogs')),
    enabled: !rangeError && !actorError,
    retry: false,
  })
  useRequestErrorToast(query.error, query.isError && !isForbidden(query.error), t, readFailureFeedback(query.error, { unavailableTitle: t('operationLog:readUnavailableTitle'), unavailableDescription: t('operationLog:readUnavailableDescription'), forbiddenTitle: t('access:forbiddenTitle'), forbiddenDescription: t('access:forbiddenDescription') }))
  const detail = useQuery({
    queryKey: ['operation-log', userID, selectedID ?? ''],
    queryFn: ({ signal }) => api.getOperationLog && selectedID ? api.getOperationLog(selectedID, signal) : Promise.reject(new Error('missing getOperationLog')),
    enabled: Boolean(selectedID),
    retry: false,
  })
  const logs = query.data?.logs ?? []
  return <section className="mx-auto flex w-full max-w-6xl flex-col gap-5" aria-labelledby="operation-log-title">
    <h1 id="operation-log-title" className="text-2xl font-semibold tracking-tight">{t('operationLog:title')}</h1>
    <Card>
      <CardHeader><CardTitle>{t('operationLog:filtersTitle')}</CardTitle></CardHeader>
      <CardContent className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div className="flex flex-col gap-2"><label htmlFor="operation-log-action" className="text-sm font-medium">{t('operationLog:action')}</label><Input id="operation-log-action" list="operation-log-actions" placeholder={t('operationLog:actionPlaceholder')} value={filters.action} onChange={(event) => setFilter('action', event.target.value)} /><datalist id="operation-log-actions">{knownActions.map((action) => <option key={action} value={action}>{actionLabel(action, t)}</option>)}</datalist></div>
        <div className="flex flex-col gap-2"><label htmlFor="operation-log-object-type" className="text-sm font-medium">{t('operationLog:objectType')}</label><Input id="operation-log-object-type" list="operation-log-object-types" placeholder={t('operationLog:objectTypePlaceholder')} value={filters.objectType} onChange={(event) => setFilter('objectType', event.target.value)} /><datalist id="operation-log-object-types">{knownObjectTypes.map((objectType) => <option key={objectType} value={objectType}>{objectLabel(objectType, t)}</option>)}</datalist></div>
        <div className="flex flex-col gap-2"><label htmlFor="operation-log-actor" className="text-sm font-medium">{t('operationLog:actorFilter')}</label><Input id="operation-log-actor" placeholder={t('operationLog:actorFilterPlaceholder')} value={filters.actorId} onChange={(event) => setFilter('actorId', event.target.value)} aria-invalid={actorError || undefined} />{actorError ? <p className="text-sm text-destructive">{t('operationLog:actorFilterError')}</p> : null}</div>
        <div className="flex flex-col gap-2"><label htmlFor="operation-log-object-id" className="text-sm font-medium">{t('operationLog:objectId')}</label><Input id="operation-log-object-id" placeholder={t('operationLog:objectIdPlaceholder')} value={filters.objectId} onChange={(event) => setFilter('objectId', event.target.value)} /></div>
        <div className="flex flex-col gap-2"><label htmlFor="operation-log-from" className="text-sm font-medium">{t('operationLog:from')}</label><Input id="operation-log-from" type="datetime-local" value={filters.from} onChange={(event) => setFilter('from', event.target.value)} /></div>
        <div className="flex flex-col gap-2"><label htmlFor="operation-log-to" className="text-sm font-medium">{t('operationLog:to')}</label><Input id="operation-log-to" type="datetime-local" value={filters.to} onChange={(event) => setFilter('to', event.target.value)} aria-invalid={rangeError || undefined} />{rangeError ? <p className="text-sm text-destructive">{t('operationLog:timeRangeError')}</p> : null}</div>
        <div className="flex flex-col gap-2"><label htmlFor="operation-log-result" className="text-sm font-medium">{t('operationLog:result')}</label><Select value={filters.result || 'all'} onValueChange={(value) => setFilter('result', value === 'all' ? '' : value as Filters['result'])}><SelectTrigger id="operation-log-result" aria-label={t('operationLog:result')}><SelectValue /></SelectTrigger><SelectContent><SelectItem value="all">{t('operationLog:allResults')}</SelectItem><SelectItem value="success">{t('operationLog:success')}</SelectItem><SelectItem value="failure">{t('operationLog:failure')}</SelectItem></SelectContent></Select></div>
        <div className="flex items-end"><Button type="button" variant="ghost" onClick={() => { setCursor(undefined); setHistory([]); setFilters(initialFilters) }}>{t('common:reset')}</Button></div>
      </CardContent>
    </Card>
    <Card>
      <CardContent className="pt-6">
        {actorError ? <p role="status" className="text-sm text-destructive">{t('operationLog:actorFilterError')}</p> : query.isPending ? <p role="status">{t('common:loading')}</p> : query.isError && isForbidden(query.error) ? <p role="status">{t('access:forbiddenDescription')}</p> : query.isError ? <p role="status">{t('operationLog:readUnavailableDescription')}</p> : logs.length === 0 ? <p className="text-sm text-muted-foreground">{t('operationLog:empty')}</p> : <div className="overflow-x-auto"><Table><TableHeader><TableRow><TableHead>{t('operationLog:occurredAt')}</TableHead><TableHead>{t('operationLog:actorName')}</TableHead><TableHead>{t('operationLog:actorEmail')}</TableHead><TableHead>{t('operationLog:action')}</TableHead><TableHead>{t('operationLog:object')}</TableHead><TableHead>{t('operationLog:result')}</TableHead><TableHead className="text-right">{t('access:actions')}</TableHead></TableRow></TableHeader><TableBody>{logs.map((log) => <TableRow key={log.id}><TableCell className="whitespace-nowrap">{formatDate(log.occurredAt)}</TableCell><TableCell className="whitespace-nowrap">{actorLabel(log, t)}</TableCell><TableCell>{log.actor.email || '—'}</TableCell><TableCell>{actionLabel(log.action, t)}</TableCell><TableCell>{targetLabel(log) || objectLabel(log.objectType, t)}{log.objectDeleted ? ` · ${t('operationLog:deletedUser')}` : ''}</TableCell><TableCell><span className={log.result === 'failure' ? 'text-destructive' : 'text-foreground'}>{log.result === 'failure' ? t('operationLog:failure') : t('operationLog:success')}</span></TableCell><TableCell className="text-right"><Button variant="ghost" size="icon" aria-label={t('operationLog:viewDetails')} onClick={() => setSelectedID(log.id)}><Eye aria-hidden="true" /></Button></TableCell></TableRow>)}</TableBody></Table></div>}
        <div className="mt-4 flex justify-end gap-2"><Button variant="outline" disabled={history.length === 0 || query.isFetching} onClick={() => { const previous = history[history.length - 1]; setHistory((current) => current.slice(0, -1)); setCursor(previous || undefined) }}>{t('access:previousPage')}</Button><Button variant="outline" disabled={!query.data?.nextCursor || query.isFetching} onClick={() => { if (!query.data?.nextCursor) return; setHistory((current) => [...current, cursor ?? '']); setCursor(query.data.nextCursor) }}>{t('access:nextPage')}</Button></div>
      </CardContent>
    </Card>
    <Dialog open={Boolean(selectedID)} onOpenChange={(open) => { if (!open) setSelectedID(undefined) }}>
      <DialogContent className="max-h-[85dvh] overflow-y-auto sm:max-w-2xl"><DialogHeader><DialogTitle>{t('operationLog:detailTitle')}</DialogTitle><DialogDescription>{t('operationLog:detailDescription')}</DialogDescription></DialogHeader>{detail.isPending ? <p role="status">{t('common:loading')}</p> : detail.isError ? <p role="status">{t('operationLog:detailUnavailable')}</p> : detail.data ? <OperationLogDetail log={detail.data} t={(key) => t(key as never)} /> : null}<DialogFooter><DialogClose asChild><Button variant="outline">{t('common:close')}</Button></DialogClose></DialogFooter></DialogContent>
    </Dialog>
  </section>
}

function OperationLogDetail({ log, t }: { log: OperationLog; t: Translator }) {
  const before = snapshot(log.details.before)
  const after = snapshot(log.details.after)
  const compared = before && after
  const changedKeys = compared ? [...new Set([...Object.keys(before), ...Object.keys(after)])].filter((key) => !technicalKeys.has(key) && JSON.stringify(before[key]) !== JSON.stringify(after[key])) : []
  const extra = Object.entries(log.details).filter(([key]) => !['before', 'after', 'target', 'failure', 'sessionCreated', 'fieldsModified'].includes(key) && !technicalKeys.has(key))
  return <div className="flex min-w-0 flex-col gap-6 text-sm">
    <div className="flex flex-wrap items-center gap-3"><p className="text-xl font-semibold">{actionLabel(log.action, t)}</p><Badge variant={log.result === 'failure' ? 'destructive' : 'secondary'}>{t(`operationLog:${log.result}`)}</Badge></div>
    {log.result === 'failure' ? <p className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-destructive">{translated(`operationLog:failureReasons.${String(log.details.failure ?? 'operation_failed')}`, String(log.details.failure ?? t('operationLog:failure')), t)}</p> : log.action === 'auth.login' ? <p>{t('operationLog:loginSummary')}</p> : null}
    <dl className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
      <DetailField label={t('operationLog:actorName')} value={actorLabel(log, t)} />
      {log.actor.email ? <DetailField label={t('operationLog:actorEmail')} value={log.actor.email} /> : null}
      <DetailField label={t('operationLog:occurredAt')} value={formatDate(log.occurredAt)} />
      {log.sourceIp ? <DetailField label={t('operationLog:sourceIp')} value={log.sourceIp} /> : null}
      {log.attemptedAccount ? <DetailField label={t('operationLog:attemptedAccount')} value={log.attemptedAccount} /> : null}
      {targetLabel(log) ? <DetailField label={t('operationLog:object')} value={`${targetLabel(log)}${log.objectDeleted ? ` · ${t('operationLog:deletedUser')}` : ''}`} /> : null}
    </dl>
    {compared ? <section className="min-w-0"><h3 className="mb-3 font-semibold">{t('operationLog:changes')}</h3>{changedKeys.length ? <Table className="table-fixed"><TableHeader><TableRow><TableHead className="w-1/4">{t('operationLog:field')}</TableHead><TableHead>{t('operationLog:beforeChange')}</TableHead><TableHead>{t('operationLog:afterChange')}</TableHead></TableRow></TableHeader><TableBody>{changedKeys.map((key) => <TableRow key={key}><TableCell className="align-top font-medium">{detailLabel(key, t)}</TableCell><TableCell className="align-top"><DetailValue field={key} value={before[key]} t={t} /></TableCell><TableCell className="align-top"><DetailValue field={key} value={after[key]} t={t} /></TableCell></TableRow>)}</TableBody></Table> : <p className="text-muted-foreground">{t('operationLog:noFieldChanges')}</p>}</section> : before || after || snapshot(log.details.target) ? <section><h3 className="mb-3 font-semibold">{t('operationLog:objectDetails')}</h3><DetailValue field="target" value={after ?? before ?? log.details.target} t={t} /></section> : null}
    {extra.length ? <dl className="grid gap-4">{extra.map(([key, value]) => <div key={key}><dt className="mb-1 text-muted-foreground">{detailLabel(key, t)}</dt><dd><DetailValue field={key} value={value} t={t} /></dd></div>)}</dl> : null}
    <details className="min-w-0 border-t pt-4 text-muted-foreground"><summary className="cursor-pointer text-sm focus-visible:outline-ring">{t('operationLog:technicalDetails')}</summary><dl className="mt-4 grid gap-3"><DetailField label={t('operationLog:recordId')} value={log.id} /><DetailField label={t('operationLog:actionCode')} value={log.action} />{log.actor.id ? <DetailField label={t('operationLog:actorFilter')} value={log.actor.id} /> : null}{log.objectId ? <DetailField label={t('operationLog:objectId')} value={log.objectId} /> : null}</dl><pre className="mt-3 max-h-60 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted p-3 text-xs">{JSON.stringify(log.details, null, 2)}</pre></details>
  </div>
}

const technicalKeys = new Set(['id', 'roleIds', 'revision', 'authVersion', 'requestedRevision', 'requestedAuthVersion'])
function snapshot(value: unknown): Record<string, unknown> | undefined { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : undefined }
function targetLabel(log: OperationLog): string {
  for (const value of [log.details.after, log.details.before, log.details.target]) {
    const item = snapshot(value)
    if (typeof item?.name === 'string' && item.name) return item.name
    if (typeof item?.email === 'string' && item.email) return item.email
  }
  return ''
}
function DetailField({ label, value }: { label: string; value: string }) { return <div className="min-w-0"><dt className="text-muted-foreground">{label}</dt><dd className="mt-1 break-words font-medium">{value}</dd></div> }

function DetailValue({ field, value, t }: { field: string; value: unknown; t: Translator }) {
  if (Array.isArray(value)) return value.length ? <ul className="space-y-2">{value.map((item, index) => <li key={index}><DetailValue field={field} value={item} t={t} /></li>)}</ul> : <span className="text-muted-foreground">{t('operationLog:emptyValue')}</span>
  const object = snapshot(value)
  if (object) return <dl className="space-y-3">{Object.entries(object).filter(([key]) => !technicalKeys.has(key)).map(([key, child]) => <div key={key}><dt className="text-muted-foreground">{detailLabel(key, t)}</dt><dd className="mt-1"><DetailValue field={key} value={child} t={t} /></dd></div>)}</dl>
  let text = value === null || value === undefined || value === '' ? t('operationLog:emptyValue') : typeof value === 'boolean' ? t(value ? 'operationLog:yes' : 'operationLog:no') : String(value)
  if (field === 'permissions' && typeof value === 'string') text = translated(`access:permission${value.split(/[-.]/).map((part) => part.charAt(0).toUpperCase() + part.slice(1)).join('')}`, value, t)
  if (field === 'passwordAction' && typeof value === 'string') text = translated(`operationLog:passwordActions.${value}`, value, t)
  if (field === 'mailQueued' && value === true) text = t('operationLog:mailQueuedExplanation')
  if (field === 'expiresAt' && typeof value === 'string') text = formatDate(value)
  return <span className="whitespace-pre-wrap break-words">{text}</span>
}

function actorLabel(log: OperationLog, t: Translator): string {
  if (log.actor.deleted) return `${log.actor.name || log.actor.id || '—'} · ${t('operationLog:deletedUser')}`
  if (log.actor.kind === 'unverified') return t('operationLog:unverifiedActor')
  if (log.actor.name) return log.actor.name
  if (log.actor.kind === 'system') return t('operationLog:systemActor')
  return log.actor.label || t('operationLog:unknownActor')
}
function translated(key: string, fallback: string, t: Translator): string {
  const value = t(key)
  return value === key || value === key.slice(key.indexOf(':') + 1) ? fallback : value
}
function actionLabel(action: string, t: Translator): string { return translated(`operationLog:actionLabels.${action.replaceAll('.', '_')}`, action, t) }
function objectLabel(objectType: string, t: Translator): string { return translated(`operationLog:objectLabels.${objectType}`, objectType, t) }
function detailLabel(key: string, t: Translator): string { return translated(`operationLog:detailLabels.${key.replaceAll('.', '_')}`, key, t) }

function toRFC3339(value: string): string { return new Date(value).toISOString() }
// Keep this in lockstep with domain.IsCanonicalUUID: UUIDv7 is valid here and
// the backend deliberately accepts canonical UUIDs without constraining the
// version or variant nibbles.
function isUUID(value: string): boolean { return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value) }
function formatDate(value: string): string { const date = new Date(value); return Number.isNaN(date.valueOf()) ? value : date.toLocaleString() }
