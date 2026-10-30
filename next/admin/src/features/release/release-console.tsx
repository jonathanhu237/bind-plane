import { Link } from '@tanstack/react-router'
import { ListChecks, Lock } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { PageHeader, isAdmin, pick, useZh } from '@/features/ops/ui'
import type { User } from '@/shared/api/contracts'
import { JobView } from './job-view'
import { canRelease } from './jobs'
import { ReleaseQueryForm } from './query-form'

/** One release at a time: the query form, then the job it queued. The job is part of the address, so a reload keeps it. */
export function ReleaseConsole({ user, jobID, onJob }: { user: User; jobID?: string; onJob: (jobID: string) => void }) {
  const zh = useZh()
  const admin = isAdmin(user)
  return <div className="mx-auto flex w-full max-w-5xl flex-col gap-8">
    <PageHeader title={pick(zh, '释放控制台', 'Release console')} description={pick(zh, '输入一个 IPv4 地址。系统先定位交换机并查询绑定，确认后才会提交释放任务。', 'Enter one IPv4 address. The switch and binding are queried before confirmation.')}>
      <Button asChild variant="outline"><Link to="/releases"><ListChecks aria-hidden="true" data-icon="inline-start" />{pick(zh, '释放记录', 'Release history')}</Link></Button>
    </PageHeader>
    {!canRelease(user)
      ? <Alert><Lock aria-hidden="true" /><AlertDescription>{pick(zh, '需要操作员或管理员角色才能使用释放控制台。', 'An Operator or Admin role is required to use the release console.')}</AlertDescription></Alert>
      : <>
        <ReleaseQueryForm admin={admin} zh={zh} allowForce onPrepared={onJob} />
        {jobID ? <JobView key={jobID} jobID={jobID} admin={admin} zh={zh} onJob={onJob} /> : null}
      </>}
  </div>
}
