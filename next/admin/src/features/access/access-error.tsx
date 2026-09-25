import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { ApiProblemError, ApiProtocolError, ApiTransportError } from '@/shared/api/client'
import { translateProblem } from '@/shared/api/problems'
import { useRequestErrorToast } from '@/shared/feedback'

export type AccessFailureKind = 'forbidden' | 'conflict' | 'validation' | 'dependency'

const conflictCodes = new Set([
  'role_in_use',
  'role_immutable',
  'last_super_admin',
  'stale_revision',
  'role_already_exists',
  'invitation_pending',
])

export function accessFailureKind(error: unknown): AccessFailureKind {
  if (error instanceof ApiProblemError) {
    if (error.problem.status === 403 || error.problem.code === 'forbidden') return 'forbidden'
    if (error.problem.status === 409 || conflictCodes.has(error.problem.code ?? '')) return 'conflict'
    if (error.problem.status === 422 || error.problem.code === 'validation_failed') return 'validation'
    return 'dependency'
  }
  if (error instanceof ApiTransportError || error instanceof ApiProtocolError) return 'dependency'
  return 'dependency'
}

type AccessErrorProps = {
  error: unknown
  descriptionOverride?: string
}

export function AccessError({ error, descriptionOverride }: AccessErrorProps) {
  const { t } = useTranslation(['access', 'common', 'problems'])
  const kind = accessFailureKind(error)
  const title = kind === 'dependency' ? t('unavailableTitle') : t(`${kind}Title`)
  const description = descriptionOverride ?? (kind === 'forbidden'
    ? t('forbiddenDescription')
    : kind === 'dependency'
      ? t('unavailableDescription')
      : translateProblem(error, t))
  return (
    <Alert variant="destructive" role="alert" aria-live="polite">
      <AlertTitle>{title}</AlertTitle>
      <AlertDescription className="flex flex-wrap items-center gap-3">
        <span>{description}</span>
      </AlertDescription>
    </Alert>
  )
}

export function AccessDenied() {
  const { t } = useTranslation(['access', 'problems'])
  const error = useMemo(() => new ApiProblemError({ type: '/problems/forbidden', title: 'forbidden', status: 403, code: 'forbidden' }), [])
  useRequestErrorToast(error, true, t, { title: t('forbiddenTitle'), description: t('forbiddenDescription') })
  return <section role="status" aria-labelledby="access-denied-title" className="flex flex-col gap-2"><h2 id="access-denied-title" className="text-lg font-semibold">{t('forbiddenTitle')}</h2><p className="text-sm text-muted-foreground">{t('forbiddenDescription')}</p></section>
}
