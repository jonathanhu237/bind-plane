import { Link, createFileRoute, useLoaderData } from '@tanstack/react-router'
import { Lock, Plus } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { OpsOverview } from '@/features/ops/overview'
import { PageHeader, isAdmin, pick, useZh } from '@/features/ops/ui'
import { RecentJobs, UncertainReleases } from '@/features/release/job-lists'
import { canRelease } from '@/features/release/jobs'

export const Route = createFileRoute('/_authenticated/')({ component: HomePage })

/** Where everyone lands: how release jobs stand and, for admins, the state of the switches. Releases themselves start in the console. */
function HomePage() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const zh = useZh()
  const admin = isAdmin(user)
  return <main className="mx-auto flex w-full max-w-6xl flex-col gap-8">
    <PageHeader title={pick(zh, '首页', 'Home')}>
      {canRelease(user) ? <Button asChild data-guide="home-new-release"><Link to="/release"><Plus aria-hidden="true" data-icon="inline-start" />{pick(zh, '发起解绑', 'New release')}</Link></Button> : null}
    </PageHeader>
    {canRelease(user) ? <>
      <UncertainReleases zh={zh} />
      <RecentJobs zh={zh} admin={admin} />
    </> : <Alert><Lock aria-hidden="true" /><AlertDescription>{pick(zh, '你的账号没有解绑权限。需要解绑 IP 时，请联系管理员分配操作员角色。', 'Your account cannot release bindings. Ask an admin for the Operator role if you need to.')}</AlertDescription></Alert>}
    {admin ? <OpsOverview zh={zh} /> : null}
  </main>
}
