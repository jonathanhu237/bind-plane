import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { RolesPage } from '@/features/access/roles-page'
import { AccessDenied } from '@/features/access/access-error'

export const Route = createFileRoute('/_authenticated/roles')({ component: RolesRoute })

function RolesRoute() {
  const { api } = Route.useRouteContext()
  const { user } = useLoaderData({ from: '/_authenticated' })
  if (!user.superAdmin && !user.permissions?.includes('roles.read')) return <AccessDenied />
  return <RolesPage api={api} canManage={Boolean(user.superAdmin || (user.permissions?.includes('roles.read') && user.permissions?.includes('roles.write')))} actorPermissions={user.permissions} actorSuperAdmin={Boolean(user.superAdmin)} />
}
