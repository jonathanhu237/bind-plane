import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { AccessDenied } from '@/features/access/access-error'
import { OperationLogsPage } from '@/features/operation-log/operation-logs-page'

export const Route = createFileRoute('/_authenticated/operation-logs')({ component: OperationLogsRoute })

function OperationLogsRoute() {
  const { api } = Route.useRouteContext()
  const { user } = useLoaderData({ from: '/_authenticated' })
  if (!user.superAdmin && !user.permissions?.includes('operation-logs.read')) return <AccessDenied />
  return <OperationLogsPage api={api} userID={user.id} />
}
