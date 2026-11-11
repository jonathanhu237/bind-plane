import { createFileRoute, isRedirect, Outlet, redirect } from '@tanstack/react-router'
import { SessionMonitor } from '@/features/auth/session-monitor'
import { AuthenticatedShell } from '@/features/auth/authenticated-shell'
import { SessionError } from '@/features/auth/session-error'
import { currentUserOptions } from '@/features/auth/queries'
import { isUnauthenticated } from '@/shared/api/problems'
import { clearAccessDrafts } from '@/features/access/drafts'
import { restoreGuestLocale } from '@/shared/i18n'

export const Route = createFileRoute('/_authenticated')({
  // The session is checked in beforeLoad, not the loader: loaders of the page being opened run in
  // parallel with this route's, so they would ask the API for data before an account still on
  // its initial password had been sent to change it.
  beforeLoad: async ({ context }) => {
    try {
      const user = await context.queryClient.fetchQuery(currentUserOptions(context.api))
      // An account on the initial password an administrator set must choose its own first.
      if (user.passwordChangeRequired) throw redirect({ to: '/change-password', replace: true })
      return { user }
    } catch (error) {
      if (isUnauthenticated(error)) {
        clearAccessDrafts()
        void restoreGuestLocale()
        context.queryClient.removeQueries({ queryKey: ['auth', 'current-user'] })
        context.queryClient.removeQueries({ queryKey: ['access'] })
        context.queryClient.removeQueries({ queryKey: ['personal-profile'] })
        context.queryClient.removeQueries({ queryKey: ['operational-warnings'] })
        context.queryClient.removeQueries({ queryKey: ['operation-log-status'] })
        context.queryClient.removeQueries({ queryKey: ['operation-logs'] })
        context.queryClient.removeQueries({ queryKey: ['operation-log'] })
        context.queryClient.removeQueries({ queryKey: ['settings'] })
        throw redirect({ to: '/login', replace: true })
      }
      if (isRedirect(error)) throw error
      throw error
    }
  },
  loader: ({ context }) => ({ user: context.user }),
  errorComponent: ({ error }) => <SessionError error={error} />,
  component: AuthenticatedRoute,
})

function AuthenticatedRoute() {
  const { user } = Route.useLoaderData()
  const { api } = Route.useRouteContext()
  return <AuthenticatedShell api={api} user={user}><SessionMonitor api={api} userID={user.id} /><Outlet /></AuthenticatedShell>
}
