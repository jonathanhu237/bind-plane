import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { AccessDenied } from '@/features/access/access-error'
import { InvitationsPage } from '@/features/access/invitations-page'

export const Route = createFileRoute('/_authenticated/invitations')({ component: InvitationsRoute })

function InvitationsRoute() {
  const { api } = Route.useRouteContext()
  const { user } = useLoaderData({ from: '/_authenticated' })
  const canView = Boolean(user.superAdmin || user.permissions?.includes('invitations.read'))
  if (!canView) return <AccessDenied />
  return <InvitationsPage api={api} canManage={Boolean(user.superAdmin || (user.permissions?.includes('invitations.write') && user.permissions?.includes('roles.read')))} actorPermissions={user.permissions} actorSuperAdmin={Boolean(user.superAdmin)} />
}
