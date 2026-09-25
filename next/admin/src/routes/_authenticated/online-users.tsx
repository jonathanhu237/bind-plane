import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { OnlineUsersPage } from '@/features/access/online-users-page'
import { AccessDenied } from '@/features/access/access-error'

export const Route = createFileRoute('/_authenticated/online-users')({ component: OnlineUsersRoute })

function OnlineUsersRoute() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { api } = Route.useRouteContext()
  if (!user.superAdmin && !user.permissions?.includes('online-users.read')) return <AccessDenied />
  return <OnlineUsersPage api={api} actorID={user.id} canManage={Boolean(user.superAdmin || user.permissions?.includes('online-users.write'))} />
}
