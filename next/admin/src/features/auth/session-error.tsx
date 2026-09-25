import { useTranslation } from 'react-i18next'
import { AuthPage } from './auth-page'
import { translateProblem } from '@/shared/api/problems'
import { useRequestErrorToast } from '@/shared/feedback'

export function SessionError({ error }: { error: unknown }) {
  const { t } = useTranslation(['auth', 'problems'])
  useRequestErrorToast(error, true, t, { title: t('sessionUnavailableTitle'), description: translateProblem(error, t) })
  return (
    <AuthPage
      title={t('sessionUnavailableTitle')}
    >
      <p role="status" className="text-sm text-muted-foreground">{t('sessionUnavailableDescription')}</p>
    </AuthPage>
  )
}
