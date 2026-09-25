import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { UsersPage } from '@/features/access/users-page'
import { AccessDenied } from '@/features/access/access-error'

export const Route = createFileRoute('/_authenticated/users')({ component: UsersRoute })

function UsersRoute() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { api } = Route.useRouteContext()
  if (!user.superAdmin && !user.permissions?.includes('users.read')) return <AccessDenied />
  return <UsersPage api={api} canManage={Boolean(user.superAdmin || (user.permissions?.includes('users.read') && user.permissions?.includes('users.write')))} actorPermissions={user.permissions} actorSuperAdmin={Boolean(user.superAdmin)} actorID={user.id} />
}
