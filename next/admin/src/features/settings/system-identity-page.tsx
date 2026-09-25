import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Image, RotateCcw, Save } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { SettingsSection } from './settings-section'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import type { SystemIdentity } from '@/shared/api/contracts'
import { isForbidden, translateProblemWithFields } from '@/shared/api/problems'
import { notifyRequestError, notifySuccess, readFailureFeedback, useRequestErrorToast } from '@/shared/feedback'
import { DefaultSystemIcon, publicSystemIdentityQueryKey } from '@/features/identity/system-identity'

type IconAction = 'preserve' | 'replace' | 'default'

type SystemIdentityDraft = {
  systemName: string
  iconAction: IconAction
  iconFile?: File
  iconPreview: string
  revision: number
  dirty: boolean
  conflict: boolean
}

export function SystemIdentityPage({ api, canWrite = true }: { api: ApiClient; canWrite?: boolean }) {
  const { t } = useTranslation(['settings', 'common', 'problems', 'access'])
  const queryClient = useQueryClient()
  const inputRef = useRef<HTMLInputElement>(null)
  const query = useQuery({
    queryKey: ['settings', 'system-identity'],
    queryFn: ({ signal }) => api.getSystemIdentity ? api.getSystemIdentity(signal) : Promise.reject(new Error('missing getSystemIdentity')),
    retry: false,
  })
  const [draft, setDraft] = useState<SystemIdentityDraft>()
  useEffect(() => {
    const preview = draft?.iconPreview
    return () => {
      if (preview?.startsWith('blob:')) URL.revokeObjectURL(preview)
    }
  }, [draft?.iconPreview])
  useRequestErrorToast(query.error, query.isError, t, readFailureFeedback(query.error, { unavailableTitle: t('identity.readUnavailableTitle'), unavailableDescription: t('identity.readUnavailableDescription'), forbiddenTitle: t('access:forbiddenTitle'), forbiddenDescription: t('access:forbiddenDescription') }))

  const current = draft?.dirty ? draft : query.data ? fromSystemIdentity(query.data) : draft ?? emptyDraft()
  const save = useMutation({
    retry: false,
    mutationFn: async () => {
      if (!api.saveSystemIdentity) throw new Error('missing saveSystemIdentity')
      const form = new FormData()
      form.append('systemName', current.systemName)
      form.append('revision', String(current.revision))
      form.append('iconAction', current.iconAction)
      if (current.iconAction === 'replace' && current.iconFile) form.append('icon', current.iconFile, current.iconFile.name)
      return api.saveSystemIdentity(form)
    },
    onSuccess: (saved) => {
      setDraft(fromSystemIdentity(saved))
      queryClient.setQueryData(['settings', 'system-identity'], saved)
      queryClient.setQueryData(publicSystemIdentityQueryKey, saved)
      void queryClient.invalidateQueries({ queryKey: ['settings', 'system-identity'] })
      void queryClient.invalidateQueries({ queryKey: publicSystemIdentityQueryKey })
      notifySuccess(t('identity.saveSuccess'))
    },
    onError: (error) => {
      const latest = draft ?? current
      setDraft({ ...latest, dirty: true, conflict: isStaleIdentityError(error) })
      notifyRequestError(error, t, isStaleIdentityError(error)
        ? { title: t('access:conflictTitle'), description: t('access:draftConflictDescription') }
        : { title: t('identity.saveFailed'), description: translateProblemWithFields(error, t) })
    },
  })
  const busy = save.isPending
  const forbidden = query.isError && isForbidden(query.error)
  const systemNameTooLong = runeLength(current.systemName) > 50
  const editDisabled = busy || !canWrite || current.conflict || query.isError
  const submitDisabled = editDisabled || !current.systemName.trim() || systemNameTooLong || (current.iconAction === 'replace' && !current.iconFile)

  if (query.isPending) return <p role="status">{t('common:loading')}</p>
  if (query.isError && forbidden && !draft) return <p role="status">{t('access:forbiddenDescription')}</p>
  if (query.isError && !draft) return <p role="status">{t('identity.readUnavailableDescription')}</p>

  const update = (patch: Partial<SystemIdentityDraft>) => setDraft({ ...current, ...patch, dirty: true, conflict: false })
  const selectFile = (file: File | undefined) => {
    if (!file) return
    const preview = URL.createObjectURL(file)
    update({ iconAction: 'replace', iconFile: file, iconPreview: preview })
  }
  const restoreDefault = () => {
    if (inputRef.current) inputRef.current.value = ''
    update({ iconAction: 'default', iconFile: undefined, iconPreview: '/api/public/system-identity/icon?default=1' })
  }
  const reloadConflict = async () => {
    const refreshed = await query.refetch()
    if (refreshed.data) setDraft(fromSystemIdentity(refreshed.data))
  }

  return <SettingsSection id="identity-settings-title" title={t('identity.title')}>
    <div className="flex flex-col gap-6">
      {current.conflict ? <Alert variant="destructive"><AlertTitle>{t('access:conflictTitle')}</AlertTitle><AlertDescription className="flex flex-wrap items-center justify-between gap-3"><span>{t('access:draftConflictDescription')}</span><Button type="button" variant="outline" size="sm" onClick={() => void reloadConflict()}>{t('access:reload')}</Button></AlertDescription></Alert> : null}
      <form className="flex flex-col gap-6" onSubmit={(event) => { event.preventDefault(); if (!submitDisabled) save.mutate() }} noValidate>
        <FieldGroup>
          <Field><FieldLabel htmlFor="system-name">{t('identity.systemName')}</FieldLabel><Input id="system-name" value={current.systemName} aria-invalid={systemNameTooLong || !current.systemName.trim()} onChange={(event) => update({ systemName: event.target.value })} disabled={editDisabled} /><FieldDescription>{t('identity.systemNameDescription')}</FieldDescription>{!current.systemName.trim() ? <FieldError>{t('identity.nameRequired')}</FieldError> : systemNameTooLong ? <FieldError>{t('identity.nameTooLong')}</FieldError> : null}</Field>
        </FieldGroup>
        <div className="grid gap-5 sm:grid-cols-[9rem_minmax(0,1fr)] sm:items-start">
          <div className="flex size-32 items-center justify-center overflow-hidden rounded-xl border bg-muted/30 p-3">{current.iconAction === 'default' || (current.iconAction === 'preserve' && !query.data?.hasCustomIcon) ? <DefaultSystemIcon className="size-16" /> : <img src={current.iconPreview} alt="" className="max-h-full max-w-full object-contain" />}</div>
          <Field><FieldLabel htmlFor="system-icon">{t('identity.icon')}</FieldLabel><Input ref={inputRef} id="system-icon" type="file" accept="image/png,image/jpeg,image/webp" onChange={(event) => selectFile(event.target.files?.[0])} disabled={editDisabled} /><FieldDescription>{t('identity.iconDescription')}</FieldDescription>{current.iconAction === 'replace' && !current.iconFile ? <FieldError>{t('identity.iconRequired')}</FieldError> : null}<div className="flex flex-wrap gap-2"><Button type="button" variant="outline" onClick={restoreDefault} disabled={editDisabled}><RotateCcw aria-hidden="true" data-icon="inline-start" />{t('identity.restoreDefault')}</Button><span className="inline-flex items-center gap-2 text-sm text-muted-foreground"><Image aria-hidden="true" className="size-4" />{current.iconAction === 'default' ? t('identity.defaultIconPending') : current.iconAction === 'replace' ? t('identity.newIconPending') : t('identity.currentIcon')}</span></div></Field>
        </div>
        {canWrite ? <div className="flex justify-end"><Button type="submit" disabled={submitDisabled}><Save aria-hidden="true" data-icon="inline-start" />{busy ? t('common:saving') : t('common:save')}</Button></div> : null}
      </form>
    </div>
  </SettingsSection>
}

const emptyDraft = (): SystemIdentityDraft => ({ systemName: 'Temvia', iconAction: 'preserve', iconPreview: '/api/public/system-identity/icon?default=1', revision: 0, dirty: false, conflict: false })

function fromSystemIdentity(identity: SystemIdentity): SystemIdentityDraft {
  return { systemName: identity.systemName, iconAction: 'preserve', iconPreview: identity.iconUrl, revision: identity.revision, dirty: false, conflict: false }
}

function runeLength(value: string): number {
  return Array.from(value.trim()).length
}

function isStaleIdentityError(error: unknown): boolean {
  return error instanceof ApiProblemError && (error.problem.code === 'stale_revision' || error.problem.type === '/problems/stale-revision')
}
