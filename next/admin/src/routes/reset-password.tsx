import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AuthPage } from '@/features/auth/auth-page'
import { PasswordResetForm } from '@/features/auth/password-reset-form'
import { clearPasswordResetAuthority, getPasswordResetAuthority } from '@/shared/bootstrap/setup-authority'

export const Route = createFileRoute('/reset-password')({
  onLeave: () => clearPasswordResetAuthority(),
  component: ResetPasswordRoute,
})

function ResetPasswordRoute() {
  const { t } = useTranslation(['auth', 'common'])
  const { t: commonT } = useTranslation('common')
  const { api } = Route.useRouteContext()
  const [authority, setAuthority] = useState(() => getPasswordResetAuthority())
  const [success, setSuccess] = useState(false)

  const invalidAuthority = () => {
    clearPasswordResetAuthority()
    setAuthority(undefined)
  }

  if (success) {
    return (
      <AuthPage title={t('resetPasswordSuccessTitle')}>
        <div className="flex flex-col gap-5">
          <p role="status" className="text-sm text-muted-foreground">{t('resetPasswordSuccessDescription')}</p>
          <Button asChild className="w-full"><a href="/login">{commonT('backToLogin')}</a></Button>
        </div>
      </AuthPage>
    )
  }
  if (!authority) {
    return (
      <AuthPage title={t('invalidResetLinkTitle')}>
        <div className="flex flex-col gap-5">
          <p role="status" className="text-sm text-muted-foreground">{t('invalidResetLinkDescription')}</p>
          <Button asChild className="w-full"><a href="/forgot-password">{t('resetAgain')}</a></Button>
        </div>
      </AuthPage>
    )
  }

  return (
    <AuthPage title={t('resetPasswordTitle')}>
      <PasswordResetForm api={api} token={authority} onSuccess={() => { clearPasswordResetAuthority(); setSuccess(true) }} onInvalidAuthority={invalidAuthority} />
      <div className="mt-4">
        <Button asChild type="button" variant="link" className="w-full"><a href="/login">{commonT('backToLogin')}</a></Button>
      </div>
    </AuthPage>
  )
}
