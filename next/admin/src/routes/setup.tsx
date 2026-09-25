import { createFileRoute, isRedirect, redirect, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AuthAlert, AuthPage } from '@/features/auth/auth-page'
import { SetupForm } from '@/features/auth/setup-form'
import { getSetupAuthority, clearSetupAuthority } from '@/shared/bootstrap/setup-authority'
import { translateProblem } from '@/shared/api/problems'
import { setupStatusOptions } from '@/features/auth/queries'
import type { AppRouterContext } from '@/app/context'

export async function loadSetupRoute({ context }: { context: AppRouterContext }) {
  if (!getSetupAuthority()) {
    throw redirect({ to: '/login', replace: true })
  }

  try {
    const status = await context.queryClient.fetchQuery(setupStatusOptions(context.api))
    if (status.status === 'complete') {
      clearSetupAuthority()
      throw redirect({ to: '/login', replace: true })
    }
    return { status, error: undefined }
  } catch (error) {
    if (isRedirect(error)) throw error
    return { status: undefined, error }
  }
}

export const Route = createFileRoute('/setup')({
  loader: loadSetupRoute,
  onLeave: () => clearSetupAuthority(),
  component: SetupRoute,
})

function SetupRoute() {
  const { t } = useTranslation(['auth', 'problems'])
  const { t: commonT } = useTranslation('common')
  const router = useRouter()
  const { api } = Route.useRouteContext()
  const { status, error } = Route.useLoaderData()
  const [authority, setAuthority] = useState(() => getSetupAuthority())

  const navigateToLogin = () => {
    setAuthority(undefined)
    void router.navigate({ to: '/login', replace: true })
  }

  if (error) {
    return (
      <AuthPage title={t('setupDependencyTitle')}>
        <div className="flex flex-col gap-5">
          <AuthAlert description={translateProblem(error, t)} />
          <Button type="button" className="w-full" onClick={() => void router.invalidate()}>{commonT('retry')}</Button>
        </div>
      </AuthPage>
    )
  }
  if (!status || !authority) return null

  return (
    <AuthPage title={t('setupTitle')}>
      <SetupForm api={api} token={authority} onSuccess={navigateToLogin} onInvalidAuthority={navigateToLogin} onSetupComplete={navigateToLogin} />
    </AuthPage>
  )
}
