import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { AccessDenied } from '@/features/access/access-error'
import { EmailTasksPage } from '@/features/email-tasks/email-tasks-page'

export const Route = createFileRoute('/_authenticated/email-tasks')({ component: EmailTasksRoute })

function EmailTasksRoute() {
  const { api } = Route.useRouteContext()
  const { user } = useLoaderData({ from: '/_authenticated' })
  if (!user.superAdmin && !user.permissions?.includes('mail-tasks.read')) return <AccessDenied />
  return <EmailTasksPage api={api} userID={user.id} canWrite={Boolean(user.superAdmin || user.permissions?.includes('mail-tasks.write'))} />
}
