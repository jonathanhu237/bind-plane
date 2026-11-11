import { actorText, ToneBadge } from '@/features/ops/ui'
import { entryLabel, lookupFound, statusLabel, statusTone, type Job } from './jobs'

export function SwitchName({ job }: { job: Pick<Job, 'switch_id' | 'switch_name' | 'switch_management_ip'> }) {
  if (!job.switch_name) return <span className="break-all font-mono">{job.switch_id || '—'}</span>
  return <>{job.switch_name}{job.switch_management_ip ? <span className="ml-2 font-mono text-muted-foreground">{job.switch_management_ip}</span> : null}</>
}

const chatNames = { wecom: ['WeCom', '企业微信'], feishu: ['Feishu', '飞书'] } as const

/** Who started a release, and the chat platform when they did it from a bot. */
export function ActorName({ job, zh }: { job: Pick<Job, 'actor_name' | 'actor_email' | 'source'>; zh: boolean }) {
  const chat = job.source && job.source !== 'console' ? chatNames[job.source] : undefined
  const via = chat ? <span className="ml-2 inline-block align-middle"><ToneBadge kind="muted">{chat[zh ? 1 : 0]}</ToneBadge></span> : null
  if (!job.actor_name) return <><span className="break-all">{actorText(undefined, job.actor_email)}</span>{via}</>
  return <>{job.actor_name}{job.actor_email ? <span className="ml-2 break-all text-muted-foreground">{job.actor_email}</span> : null}{via}</>
}

export function Info({ label, children, className }: { label: string; children: React.ReactNode; className?: string }) {
  return <div className="min-w-0"><dt className="text-muted-foreground">{label}</dt><dd className={className}>{children}</dd></div>
}

/** A release's status; for a finished lookup, what it found. */
export function JobStatusBadge({ job, zh }: { job: Pick<Job, 'status'> & Partial<Pick<Job, 'kind' | 'before_state'>>; zh: boolean }) {
  const found = lookupFound({ kind: job.kind ?? 'release', status: job.status, before_state: job.before_state })
  if (found) return <ToneBadge kind="muted">{entryLabel(found, zh)}</ToneBadge>
  // A lookup never releases anything, so it never says Released, even without what it found.
  if (job.kind === 'lookup' && job.status === 'succeeded') return <ToneBadge kind="muted">{zh ? '已查询' : 'Looked up'}</ToneBadge>
  return <ToneBadge kind={statusTone(job.status)}>{statusLabel(job.status, zh)}</ToneBadge>
}
