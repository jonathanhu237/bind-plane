import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ChevronRight, CircleAlert, TriangleAlert } from 'lucide-react'
import { useEffect, useId, useState } from 'react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible'
import { Field, FieldLabel } from '@/components/ui/field'
import { Skeleton } from '@/components/ui/skeleton'
import { formatTime } from '@/features/ops/ui'
import { Info, JobStatusBadge, SwitchName } from './job-parts'
import {
  assetOptionsKey, canRetry, entryLabel, isRunning, jobListKey, phaseLabel, rawOutputLabel, rawOutputs, reasonLabel, releaseErrorText, request, statusLabel,
  type AssetOptions, type Job,
} from './jobs'
import { serverText } from './messages'

type Props = { jobID: string; admin: boolean; zh: boolean; onJob: (jobID: string) => void }

/** One release job: a pre-release query and its confirmation, or a release and its outcome. */
export function JobView({ jobID, admin, zh, onJob }: Props) {
  const id = useId()
  const queryClient = useQueryClient()
  const job = useQuery({ queryKey: ['release-job', jobID], queryFn: () => request<Job>(`/api/releases/jobs/${jobID}`),
    refetchInterval: (query) => (isRunning(query.state.data) ? 1800 : false) })
  const data = job.data
  const options = useQuery({ queryKey: [assetOptionsKey], queryFn: () => request<AssetOptions>('/api/releases/admin/assets'), enabled: admin && data?.kind === 'release', staleTime: 60_000 })
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [force, setForce] = useState(false)
  const entry = data?.before_state?.entry_type
  const settled = data?.status === 'waiting_confirmation' || data?.status === 'cancelled'
  // A forced query that found no record arrives with the force switch already on.
  useEffect(() => { if (settled) setForce(entry === 'missing' && Boolean(data?.force)) }, [settled, entry, data?.force])
  const labels = zh ? {
    state: '当前状态', release: '解绑', check: '核对以下信息后确认解绑。', switch: '交换机', reason: '解绑原因', entryType: '绑定类型', queriedAt: '查询时间',
    confirm: '确认解绑', retry: '重新查询', force: '管理员强制解绑（查询无记录）', confirmed: '这次查询已确认解绑。', viewRelease: '查看解绑结果',
    noRecord: '交换机上没有这个 IP 的记录，解绑已停止。确需强制解绑时请联系管理员。',
    actor: '执行人', profile: '命令模板', before: '解绑前', finalState: '最终状态', forced: '强制解绑', finishedAt: '完成时间', yes: '是', no: '否',
    retryOf: '重试自', original: '查看原任务', events: '执行过程',
    uncertain: '结果不确定。同一 IP 的后续解绑已暂停；管理员必须先核实原执行器已停止及交换机现状，不要重复发出解绑命令。',
  } : {
    state: 'Current state', release: 'Release', check: 'Check the details below, then confirm the release.', switch: 'Switch', reason: 'Reason', entryType: 'Entry type', queriedAt: 'Queried',
    confirm: 'Confirm static binding release', retry: 'Retry query', force: 'Admin force release (no record found)', confirmed: 'This query was confirmed for release.', viewRelease: 'View the release',
    noRecord: 'The switch has no record of this IP, so the release stopped. Ask an admin if it must be forced.',
    actor: 'Actor', profile: 'Command profile', before: 'Before release', finalState: 'Final state', forced: 'Forced', finishedAt: 'Finished', yes: 'Yes', no: 'No',
    retryOf: 'Retry of', original: 'View the original job', events: 'Progress',
    uncertain: 'Outcome uncertain. Further releases for this IP are blocked. An admin must verify the old executor has stopped and inspect the switch before any new command.',
  }

  async function act(path: string, body: unknown) {
    setBusy(true); setError('')
    try {
      const next = await request<Job>(path, body)
      void queryClient.invalidateQueries({ queryKey: [jobListKey] })
      onJob(next.job_id || next.id)
    } catch (cause) { setError(releaseErrorText(cause, zh)) }
    finally { setBusy(false) }
  }

  if (job.error) return <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{releaseErrorText(job.error, zh)}</AlertDescription></Alert>
  if (!data) return <Skeleton className="h-56 rounded-xl" />
  const message = serverText(data.result?.message || data.error_message, zh)
  const jobLink = (target: string, text: string) => <Button asChild variant="link" className="h-auto p-0"><Link to="/release" search={{ job: target }}>{text}</Link></Button>
  const retryOf = data.retry_of_id ? <Info label={labels.retryOf}>{jobLink(data.retry_of_id, labels.original)}</Info> : null
  const retry = canRetry(data, admin) ? <Button type="button" variant="outline" disabled={busy} onClick={() => void act(`/api/releases/jobs/${data.id}/retry`, {})}>{labels.retry}</Button> : null
  const errorNote = error ? <Alert variant="destructive"><CircleAlert aria-hidden="true" /><AlertDescription>{error}</AlertDescription></Alert> : null
  const events = data.events?.length ? <div className="flex flex-col gap-1">
    <h3 className="text-sm font-medium">{labels.events}</h3>
    <ol className="flex flex-col gap-1 text-xs text-muted-foreground">{data.events.map((event, index) => <li key={`${event.created_at}-${index}`}>
      <time dateTime={event.created_at} className="tabular-nums">{formatTime(event.created_at, zh)}</time> · {phaseLabel(event.phase, zh)} · {statusLabel(event.status, zh)} {serverText(event.message, zh)}
    </li>)}</ol>
  </div> : null
  const raw = rawOutputs.map((key) => data[key] ? <Collapsible key={key}>
    <CollapsibleTrigger asChild>
      <Button type="button" variant="ghost" size="sm" className="group -ml-3"><ChevronRight aria-hidden="true" data-icon="inline-start" className="transition-transform group-data-[state=open]:rotate-90" />{rawOutputLabel(key, zh)}</Button>
    </CollapsibleTrigger>
    <CollapsibleContent><pre className="mt-2 overflow-x-auto rounded-md bg-muted p-3 font-mono text-xs">{data[key]}</pre></CollapsibleContent>
  </Collapsible> : null)

  if (data.kind !== 'release') {
    const stoppedNoRecord = data.status === 'cancelled' && entry === 'missing'
    const forceable = admin && !data.release_job_id && entry === 'missing' && (data.status === 'waiting_confirmation' || stoppedNoRecord)
    const ready = !data.release_job_id && (data.status === 'waiting_confirmation' && entry === 'static' && !force || forceable && force)
    const description = isRunning(data) ? phaseLabel(data.phase, zh) : message || (ready ? labels.check : '')
    return <Card aria-live="polite">
      <CardHeader className="flex-row flex-wrap items-start justify-between gap-3">
        <div className="flex flex-col gap-1.5">
          <CardTitle>{labels.state}</CardTitle>
          {description ? <CardDescription>{description}</CardDescription> : null}
        </div>
        <JobStatusBadge job={data} zh={zh} />
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          <Info label="IP" className="font-mono">{data.target_ip}</Info>
          <Info label={labels.switch}><SwitchName job={data} /></Info>
          <Info label="MAC" className="font-mono">{data.before_state?.mac ?? '—'}</Info>
          <Info label={labels.entryType}>{entryLabel(entry, zh)}</Info>
          <Info label={labels.reason}>{reasonLabel(data.reason, zh)}</Info>
          <Info label={labels.queriedAt} className="tabular-nums">{formatTime(data.created_at, zh)}</Info>
          {retryOf}
        </dl>
        {stoppedNoRecord && !admin ? <Alert variant="warning"><TriangleAlert aria-hidden="true" /><AlertDescription>{labels.noRecord}</AlertDescription></Alert> : null}
        {data.release_job_id ? <p className="text-sm">{labels.confirmed} {jobLink(data.release_job_id, labels.viewRelease)}</p> : null}
        {forceable ? <Field orientation="horizontal">
          <Checkbox id={`${id}-force`} checked={force} onCheckedChange={(checked) => setForce(checked === true)} />
          <FieldLabel htmlFor={`${id}-force`} className="font-normal">{labels.force}</FieldLabel>
        </Field> : null}
        {errorNote}
        {ready || retry ? <div className="flex flex-wrap gap-2">
          {ready ? <Button type="button" disabled={busy} onClick={() => void act('/api/releases', { preparation_job_id: data.id, force })}>{labels.confirm}</Button> : null}
          {retry}
        </div> : null}
        {events}
        {raw}
      </CardContent>
    </Card>
  }

  const profile = options.data?.profiles.find((item) => item.id === data.command_profile_id)?.name ?? '—'
  return <Card aria-live="polite">
    <CardHeader className="flex-row flex-wrap items-start justify-between gap-3">
      <div className="flex flex-col gap-1.5">
        <CardTitle>{labels.release} · <span className="font-mono">{data.target_ip}</span></CardTitle>
        <CardDescription>{message || phaseLabel(data.phase, zh)}</CardDescription>
      </div>
      <JobStatusBadge job={data} zh={zh} />
    </CardHeader>
    <CardContent className="flex flex-col gap-4">
      {data.status === 'needs_manual_confirmation' ? <Alert variant="warning"><TriangleAlert aria-hidden="true" /><AlertDescription>{labels.uncertain}</AlertDescription></Alert> : null}
      <dl className="grid gap-3 text-sm sm:grid-cols-2">
        <Info label={labels.switch}><SwitchName job={data} /></Info>
        <Info label={labels.actor}>{data.actor_email ?? '—'}</Info>
        <Info label={labels.before}>{entryLabel(data.before_state?.entry_type, zh)}{data.before_state?.mac ? <span className="ml-2 font-mono text-muted-foreground">{data.before_state.mac}</span> : null}</Info>
        <Info label={labels.finalState}>{entryLabel(data.after_state?.entry_type, zh)}</Info>
        <Info label={labels.reason}>{reasonLabel(data.reason, zh)}</Info>
        <Info label={labels.forced}>{data.force ? labels.yes : labels.no}</Info>
        {admin ? <Info label={labels.profile}>{profile}</Info> : null}
        <Info label={labels.finishedAt} className="tabular-nums">{formatTime(data.finished_at, zh)}</Info>
        {retryOf}
      </dl>
      {errorNote}
      {retry ? <div>{retry}</div> : null}
      {events}
      {raw}
    </CardContent>
  </Card>
}
