import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AuthPage } from '@/features/auth/auth-page'
import { ForcedPasswordChangeForm } from '@/features/auth/forced-password-change-form'
import { currentUserOptions } from '@/features/auth/queries'
import { clearAccessDrafts } from '@/features/access/drafts'
import { isUnauthenticated } from '@/shared/api/problems'
import { notifySuccess } from '@/shared/feedback'
import { restoreGuestLocale } from '@/shared/i18n'

// Where an account still on the initial password an administrator set lands after signing in:
// it can only choose its own password here, or sign out.
export const Route = createFileRoute('/change-password')({
  loader: async ({ context }) => {
    try {
      const user = await context.queryClient.fetchQuery(currentUserOptions(context.api))
      if (!user.passwordChangeRequired) throw redirect({ to: '/', replace: true })
      return { user }
    } catch (error) {
      if (isUnauthenticated(error)) throw redirect({ to: '/login', replace: true })
      throw error
    }
  },
  component: ChangePasswordRoute,
})

function ChangePasswordRoute() {
  const { t } = useTranslation('auth')
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { api } = Route.useRouteContext()
  const { user } = Route.useLoaderData()
  // Changing the password ends every session, this one too; signing out ends it as well.
  const leave = (message: string) => {
    clearAccessDrafts()
    queryClient.clear()
    void restoreGuestLocale()
    notifySuccess(message)
    void navigate({ to: '/login', replace: true })
  }
  const logout = useMutation({ retry: false, mutationFn: () => api.logout(), onSuccess: () => leave(t('logoutSuccess')) })
  return (
    <AuthPage title={t('forcedPasswordTitle')}>
      <div className="flex flex-col gap-6">
        <p className="text-sm text-muted-foreground">{t('forcedPasswordDescription')}</p>
        <p className="text-sm"><span className="font-medium">{user.name}</span> <span className="text-muted-foreground">{user.email}</span></p>
        <ForcedPasswordChangeForm api={api} onChanged={() => leave(t('passwordChangedSignIn'))} />
        <Button type="button" variant="ghost" className="w-full" disabled={logout.isPending} onClick={() => logout.mutate()}>{logout.isPending ? t('loggingOut') : t('signOut')}</Button>
      </div>
    </AuthPage>
  )
}
