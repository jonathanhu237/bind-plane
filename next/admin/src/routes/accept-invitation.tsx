import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { AuthPage } from '@/features/auth/auth-page'
import { InvitationAcceptanceForm } from '@/features/access/invitation-acceptance-form'
import { clearInvitationAuthority, getInvitationAuthority } from '@/shared/bootstrap/setup-authority'

export const Route = createFileRoute('/accept-invitation')({ onLeave: () => clearInvitationAuthority(), component: AcceptInvitationRoute })

function AcceptInvitationRoute() {
  const { t } = useTranslation(['access', 'common'])
  const { api } = Route.useRouteContext()
  const [authority, setAuthority] = useState(() => getInvitationAuthority())
  const [success, setSuccess] = useState(false)
  if (success) return <AuthPage title={t('invitationAccepted')}><div className="flex flex-col gap-5"><p role="status" className="text-sm text-muted-foreground">{t('invitationAcceptedDescription')}</p><Button asChild className="w-full"><a href="/login">{t('common:backToLogin')}</a></Button></div></AuthPage>
  if (!authority) return <AuthPage title={t('invalidInvitationTitle')}><div className="flex flex-col gap-5"><p role="status" className="text-sm text-muted-foreground">{t('invalidInvitationDescription')}</p><Button asChild className="w-full"><a href="/login">{t('common:backToLogin')}</a></Button></div></AuthPage>
  return <AuthPage title={t('invitationTitle')}><InvitationAcceptanceForm api={api} token={authority} onSuccess={() => { clearInvitationAuthority(); setSuccess(true) }} onInvalid={() => { clearInvitationAuthority(); setAuthority(undefined) }} /></AuthPage>
}
