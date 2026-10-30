import { Link, createFileRoute, useLoaderData, useNavigate } from '@tanstack/react-router'
import { Lock } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { OpsOverview } from '@/features/ops/overview'
import { PageHeader, isAdmin, pick, useZh } from '@/features/ops/ui'
import { RecentJobs, UncertainReleases } from '@/features/release/job-lists'
import { canRelease } from '@/features/release/jobs'
import { ReleaseQueryForm } from '@/features/release/query-form'

export const Route = createFileRoute('/_authenticated/')({ component: HomePage })

/** Where everyone lands: start a release, follow up on jobs, and for admins the state of the switches. */
function HomePage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const navigate = useNavigate()
  const admin = isAdmin(user)
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-8">
    <PageHeader title={pick(zh, '首页', 'Home')} description={admin
      ? pick(zh, '发起解绑、跟进解绑任务，并查看交换机的运维状态。', 'Start a release, follow up on release jobs and check on your switches.')
      : pick(zh, '发起解绑，并跟进你的解绑任务。', 'Start a release and follow up on your release jobs.')} />
    {canRelease(user) ? <>
      <ReleaseQueryForm admin={admin} zh={zh}
        title={pick(zh, '解绑 IP', 'Release a static binding')}
        description={pick(zh, '输入一个 IPv4 地址，先查询交换机上的绑定，确认后才会解绑。', 'Enter one IPv4 address. Its binding on the switch is queried first; nothing is released until you confirm.')}
        action={<Link to="/release" className="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{pick(zh, '打开解绑控制台', 'Open the release console')}</Link>}
        onPrepared={(job) => void navigate({ to: '/release', search: { job } })} />
      <UncertainReleases zh={zh} admin={admin} />
      <RecentJobs zh={zh} admin={admin} />
    </> : <Alert><Lock aria-hidden="true" /><AlertDescription>{pick(zh, '你的账号没有解绑权限。需要解绑 IP 时，请联系管理员分配操作员角色。', 'Your account cannot release bindings. Ask an admin for the Operator role if you need to.')}</AlertDescription></Alert>}
    {admin ? <OpsOverview zh={zh} /> : null}
  </main>
}
