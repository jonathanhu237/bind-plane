import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Save } from 'lucide-react'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { SettingsSection } from './settings-section'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { isForbidden, translateProblemWithFields } from '@/shared/api/problems'
import { notifyRequestError, notifySuccess, readFailureFeedback, useRequestErrorToast } from '@/shared/feedback'
import { nextDraftSubmissionID, useAccessDraftStore, type OperationLogRetentionDraft } from '@/features/access/drafts'

const emptyDraft: OperationLogRetentionDraft = { retentionDays: 180, revision: 0, submitting: false, conflict: false, authoritative: false }

export function OperationLogRetentionCard({ api, userID = 'unknown', canWrite = true }: { api: ApiClient; userID?: string; canWrite?: boolean }) {
  const { t } = useTranslation(['settings', 'common', 'access', 'operationLog'])
  const queryClient = useQueryClient()
  const draft = useAccessDraftStore((state) => state.operationLogRetention)
  const setDraft = useAccessDraftStore((state) => state.setOperationLogRetention)
  const query = useQuery({ queryKey: ['settings', 'operation-log-retention', userID], queryFn: ({ signal }) => api.getOperationLogRetention ? api.getOperationLogRetention(signal) : Promise.reject(new Error('missing getOperationLogRetention')), retry: false })
  useRequestErrorToast(query.error, query.isError, t, readFailureFeedback(query.error, { unavailableTitle: t('operationLog:retentionReadUnavailableTitle'), unavailableDescription: t('operationLog:retentionReadUnavailableDescription'), forbiddenTitle: t('access:forbiddenTitle'), forbiddenDescription: t('access:forbiddenDescription') }))

  useEffect(() => {
    if (draft && draft.ownerID && draft.ownerID !== userID) {
      setDraft(undefined)
      return
    }
    if (!query.data) return
    if (!draft || draft.ownerID !== userID || !draft.authoritative) setDraft({ retentionDays: query.data.retentionDays, revision: query.data.revision, submitting: draft?.submitting ?? false, conflict: false, authoritative: true, ownerID: userID, submissionID: draft?.submissionID })
  }, [draft, query.data, setDraft, userID])

  const current = draft && (!draft.ownerID || draft.ownerID === userID) ? draft : emptyDraft
  const authoritative = Boolean(query.data || current.authoritative)
  const currentValid = Number.isFinite(current.retentionDays) && Number.isInteger(current.retentionDays) && current.retentionDays >= 1 && current.retentionDays <= 3650
  const busy = current.submitting
  const save = useMutation({
    retry: false,
    mutationFn: async ({ retentionDays, revision, submissionID }: { retentionDays: number; revision: number; submissionID: string }) => {
      if (!api.saveOperationLogRetention) throw new Error('missing saveOperationLogRetention')
      return { result: await api.saveOperationLogRetention({ retentionDays, revision }), submissionID }
    },
    onMutate: ({ submissionID }) => {
      setDraft({ ...current, ownerID: userID, authoritative: true, submissionID, submitting: true })
      return { submissionID }
    },
    onSuccess: ({ result: saved, submissionID }) => {
      const latest = useAccessDraftStore.getState().operationLogRetention
      if (!latest || latest.ownerID !== userID || latest.submissionID !== submissionID) return
      setDraft({ retentionDays: saved.retentionDays, revision: saved.revision, submitting: false, conflict: false, authoritative: true, ownerID: userID })
      notifySuccess(t('operationLog:retentionSaveSuccess'))
      void queryClient.invalidateQueries({ queryKey: ['settings', 'operation-log-retention', userID] })
    },
    onError: (error, _variables, context) => {
      const latest = useAccessDraftStore.getState().operationLogRetention
      if (!latest || latest.ownerID !== userID || latest.submissionID !== context?.submissionID) return
      const conflict = isStaleRetentionError(error)
      setDraft({ ...latest, submitting: false, conflict, authoritative: latest.authoritative, ownerID: userID })
      notifyRequestError(error, t, conflict ? { title: t('access:conflictTitle'), description: t('access:draftConflictDescription') } : { title: t('operationLog:retentionSaveFailed'), description: translateProblemWithFields(error, t) })
    },
  })
  const forbidden = query.isError && isForbidden(query.error)
  const rangeWarning = Boolean(query.data && current.retentionDays < query.data.retentionDays)
  const editDisabled = busy || !canWrite || !authoritative || query.isError
  const submitDisabled = busy || !canWrite || !authoritative || query.isError || !currentValid || current.conflict
  const update = (retentionDays: number) => setDraft({ ...current, ownerID: userID, authoritative, retentionDays, conflict: false, submissionID: undefined })
  const discardConflict = async () => {
    const conflicted = useAccessDraftStore.getState().operationLogRetention
    if (!conflicted || conflicted.ownerID !== userID || !conflicted.conflict) return
    const submissionID = conflicted.submissionID
    const refreshed = await query.refetch()
    if (refreshed.isError || !refreshed.data) return
    const latest = useAccessDraftStore.getState().operationLogRetention
    if (!latest || latest.ownerID !== userID || latest.conflict !== true || latest.submissionID !== submissionID) return
    setDraft({ retentionDays: refreshed.data.retentionDays, revision: refreshed.data.revision, submitting: false, conflict: false, authoritative: true, ownerID: userID, submissionID: undefined })
  }
  return <SettingsSection id="retention-settings-title" title={t('operationLog:retentionTitle')}>
      {query.isError ? <Alert variant={forbidden ? 'default' : 'destructive'}><AlertTitle>{forbidden ? t('access:forbiddenTitle') : t('operationLog:retentionReadUnavailableTitle')}</AlertTitle><AlertDescription>{forbidden ? t('access:forbiddenDescription') : t('operationLog:retentionReadUnavailableDescription')}</AlertDescription></Alert> : null}
      <form className="flex flex-col gap-6" onSubmit={(event) => { event.preventDefault(); if (!submitDisabled) save.mutate({ retentionDays: current.retentionDays, revision: current.revision, submissionID: nextDraftSubmissionID() }) }} noValidate>
        {current.conflict ? <Alert variant="destructive"><AlertTitle>{t('access:conflictTitle')}</AlertTitle><AlertDescription className="flex flex-wrap items-center justify-between gap-3"><span>{t('access:draftConflictDescription')}</span>{query.data ? <Button type="button" variant="outline" size="sm" onClick={discardConflict}>{t('access:discardDraft')}</Button> : null}</AlertDescription></Alert> : null}
        {rangeWarning ? <Alert variant="destructive"><AlertTitle>{t('operationLog:retentionShorteningTitle')}</AlertTitle><AlertDescription>{t('operationLog:retentionShorteningDescription')}</AlertDescription></Alert> : null}
        <FieldGroup>
        <Field data-invalid={!currentValid || undefined}>
          <FieldLabel htmlFor="operation-log-retention-days">{t('operationLog:retentionDays')}</FieldLabel>
          <Input id="operation-log-retention-days" className="max-w-32" aria-describedby="retention-description" type="number" min={1} max={3650} step={1} value={current.retentionDays} onChange={(event) => update(Number(event.target.value))} disabled={editDisabled} aria-invalid={!currentValid} />
          <FieldDescription id="retention-description">{t('operationLog:retentionDescription')}</FieldDescription>
          {!currentValid ? <FieldError>{t('operationLog:retentionDaysError')}</FieldError> : null}
        </Field>
        </FieldGroup>
        {canWrite ? <div className="flex justify-end"><Button type="submit" disabled={submitDisabled}><Save aria-hidden="true" data-icon="inline-start" />{busy ? t('common:saving') : t('common:save')}</Button></div> : null}
      </form>
  </SettingsSection>
}

function isStaleRetentionError(error: unknown): boolean {
  return error instanceof ApiProblemError && (error.problem.code === 'stale_revision' || error.problem.type === '/problems/stale-revision')
}
