import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AuthPage } from '@/features/auth/auth-page'
import { LoginForm } from '@/features/auth/login-form'
import { currentUserOptions } from '@/features/auth/queries'

export const Route = createFileRoute('/login')({
  component: LoginRoute,
})

function LoginRoute() {
  const { t } = useTranslation('auth')
  const navigate = useNavigate()
  const { api } = Route.useRouteContext()
  const currentUser = useQuery(currentUserOptions(api))

  useEffect(() => {
    if (currentUser.data) void navigate({ to: '/', replace: true })
  }, [currentUser.data, navigate])

  return (
    <AuthPage title={t('loginTitle')}>
      <LoginForm api={api} onSuccess={() => void navigate({ to: '/', replace: true })} />
    </AuthPage>
  )
}
