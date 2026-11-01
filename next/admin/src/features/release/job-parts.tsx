import { actorText, ToneBadge } from '@/features/ops/ui'
import { statusLabel, statusTone, type Job } from './jobs'

export function SwitchName({ job }: { job: Pick<Job, 'switch_id' | 'switch_name' | 'switch_management_ip'> }) {
  if (!job.switch_name) return <span className="break-all font-mono">{job.switch_id || '—'}</span>
  return <>{job.switch_name}{job.switch_management_ip ? <span className="ml-2 font-mono text-muted-foreground">{job.switch_management_ip}</span> : null}</>
}

export function ActorName({ job }: { job: Pick<Job, 'actor_name' | 'actor_email'> }) {
  if (!job.actor_name) return <span className="break-all">{actorText(undefined, job.actor_email)}</span>
  return <>{job.actor_name}{job.actor_email ? <span className="ml-2 break-all text-muted-foreground">{job.actor_email}</span> : null}</>
}

export function Info({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return <div className="min-w-0"><dt className="text-muted-foreground">{label}</dt><dd className={className}>{children}</dd></div>
}

export function JobStatusBadge({ job, zh }: { job: Pick<Job, 'status'>; zh: boolean }) {
  return <ToneBadge kind={statusTone(job.status)}>{statusLabel(job.status, zh)}</ToneBadge>
}
