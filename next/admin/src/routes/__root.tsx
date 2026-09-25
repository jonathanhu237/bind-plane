import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AuthPage } from '@/features/auth/auth-page'
import type { AppRouterContext } from '@/app/context'

export const Route = createRootRouteWithContext<AppRouterContext>()({
  component: () => <Outlet />,
  notFoundComponent: NotFoundPage,
  errorComponent: RootErrorPage,
})

function NotFoundPage() {
  const { t } = useTranslation(['common', 'problems'])
  return (
    <AuthPage title={t('problems:notFound')}>
      <Button asChild className="w-full"><a href="/login">{t('backToLogin')}</a></Button>
    </AuthPage>
  )
}

function RootErrorPage({ reset }: { error: unknown; reset: () => void }) {
  const { t } = useTranslation(['common', 'problems'])
  return (
    <AuthPage title={t('problems:generic')}>
      <Button type="button" className="w-full" onClick={reset}>{t('retry')}</Button>
    </AuthPage>
  )
}
