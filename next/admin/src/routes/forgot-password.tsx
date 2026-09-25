import { createFileRoute, useNavigate } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AuthPage } from '@/features/auth/auth-page'
import { PasswordResetRequestForm } from '@/features/auth/password-reset-request-form'

export const Route = createFileRoute('/forgot-password')({
  component: ForgotPasswordRoute,
})

function ForgotPasswordRoute() {
  const { t } = useTranslation(['auth', 'common'])
  const { t: commonT } = useTranslation('common')
  const navigate = useNavigate()
  const { api } = Route.useRouteContext()
  const [accepted, setAccepted] = useState(false)

  if (accepted) {
    return (
      <AuthPage title={t('resetRequestAcceptedTitle')}>
        <div className="flex flex-col gap-4">
          <p role="status" className="text-sm text-muted-foreground">{t('resetRequestAcceptedDescription')}</p>
          <Button type="button" className="w-full" onClick={() => setAccepted(false)}>{t('resetAgain')}</Button>
          <Button asChild type="button" variant="outline" className="w-full"><a href="/login">{commonT('backToLogin')}</a></Button>
        </div>
      </AuthPage>
    )
  }

  return (
    <AuthPage title={t('forgotPasswordTitle')}>
      <div className="flex flex-col gap-5">
        <PasswordResetRequestForm api={api} onAccepted={() => setAccepted(true)} />
        <Button type="button" variant="link" className="w-full" onClick={() => void navigate({ to: '/login' })}>{commonT('backToLogin')}</Button>
      </div>
    </AuthPage>
  )
}
