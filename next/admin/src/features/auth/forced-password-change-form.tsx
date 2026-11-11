import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { fieldProblemFor, translateProblemWithFields } from '@/shared/api/problems'
import { forcedPasswordChangeFormSchema, normalizePassword, type ForcedPasswordChangeFormValues } from './schemas'
import { PasswordField } from './form-fields'

/**
 * Replaces the initial password an administrator set. Changing a password signs every session
 * out, so onChanged sends the person back to sign in with the new one.
 */
export function ForcedPasswordChangeForm({ api, onChanged }: { api: ApiClient; onChanged: () => void }) {
  const { t } = useTranslation(['auth', 'problems'])
  const [formError, setFormError] = useState<unknown>()
  const form = useForm<ForcedPasswordChangeFormValues>({
    resolver: zodResolver(forcedPasswordChangeFormSchema),
    mode: 'onBlur',
    shouldFocusError: true,
    defaultValues: { currentPassword: '', newPassword: '', confirmPassword: '' },
  })
  const mutation = useMutation({
    retry: false,
    mutationFn: (values: ForcedPasswordChangeFormValues) => {
      if (!api.changePersonalPassword) throw new Error('missing changePersonalPassword')
      return api.changePersonalPassword({ currentPassword: values.currentPassword, newPassword: normalizePassword(values.newPassword), confirmPassword: normalizePassword(values.confirmPassword) })
    },
    onSuccess: onChanged,
  })

  const submit = form.handleSubmit(async (values) => {
    setFormError(undefined)
    try {
      await mutation.mutateAsync(values)
    } catch (error) {
      if (error instanceof ApiProblemError) {
        let focused = false
        for (const fieldName of ['currentPassword', 'newPassword', 'confirmPassword'] as const) {
          const field = fieldProblemFor(error, `/${fieldName}`)
          if (field) {
            form.setError(fieldName, { type: 'server', message: field.code }, { shouldFocus: !focused })
            focused = true
          }
        }
      }
      setFormError(error)
    }
  })

  return (
    <form noValidate onSubmit={(event) => void submit(event)} className="flex flex-col gap-6">
      {formError !== undefined ? <Alert variant="destructive" role="alert" aria-live="polite"><AlertDescription>{translateProblemWithFields(formError, t)}</AlertDescription></Alert> : null}
      <FieldGroup className="gap-5">
        <PasswordField id="currentPassword" label={t('initialPasswordLabel')} registration={form.register('currentPassword')} error={form.formState.errors.currentPassword} autoComplete="current-password" />
        <PasswordField id="newPassword" label={t('newPasswordLabel')} registration={form.register('newPassword')} error={form.formState.errors.newPassword} autoComplete="new-password" />
        <PasswordField id="confirmPassword" label={t('confirmNewPasswordLabel')} registration={form.register('confirmPassword')} error={form.formState.errors.confirmPassword} autoComplete="new-password" />
      </FieldGroup>
      <Button type="submit" className="w-full" disabled={mutation.isPending}>
        {mutation.isPending ? t('changingPassword') : t('changePassword')}
      </Button>
    </form>
  )
}
