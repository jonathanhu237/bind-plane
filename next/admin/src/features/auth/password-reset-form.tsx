import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { fieldProblemFor, isInvalidPasswordResetToken, translateRateLimitedProblem } from '@/shared/api/problems'
import { normalizePasswordResetValues, passwordResetFormSchema, type PasswordResetFormValues } from './schemas'
import { PasswordField } from './form-fields'

export function PasswordResetForm({ api, token, onSuccess, onInvalidAuthority }: { api: ApiClient; token: string; onSuccess: () => void; onInvalidAuthority: () => void }) {
  const { t } = useTranslation(['auth', 'problems'])
  const [formError, setFormError] = useState<unknown>()
  const form = useForm<PasswordResetFormValues>({
    resolver: zodResolver(passwordResetFormSchema),
    mode: 'onBlur',
    shouldFocusError: true,
    defaultValues: { password: '', passwordConfirmation: '' },
  })
  const mutation = useMutation({
    retry: false,
    mutationFn: (values: PasswordResetFormValues) => api.completePasswordReset({ token, ...normalizePasswordResetValues(values) }),
    onSuccess,
  })

  const submit = form.handleSubmit(async (values) => {
    setFormError(undefined)
    try {
      await mutation.mutateAsync(values)
    } catch (error) {
      if (error instanceof ApiProblemError && isInvalidPasswordResetToken(error)) {
        onInvalidAuthority()
        return
      }
      if (error instanceof ApiProblemError) {
        let focused = false
        for (const fieldName of ['password', 'passwordConfirmation'] as const) {
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
      {formError !== undefined ? <Alert variant="destructive" role="alert" aria-live="polite"><AlertDescription>{translateRateLimitedProblem(formError, t, 'passwordResetCompletionRateLimited')}</AlertDescription></Alert> : null}
      <FieldGroup className="gap-5">
        <PasswordField id="password" label={t('passwordLabel')} registration={form.register('password')} error={form.formState.errors.password} autoComplete="new-password" />
        <PasswordField id="passwordConfirmation" label={t('confirmPasswordLabel')} registration={form.register('passwordConfirmation')} error={form.formState.errors.passwordConfirmation} autoComplete="new-password" />
      </FieldGroup>
      <Button type="submit" className="w-full" disabled={mutation.isPending}>
        {mutation.isPending ? t('resettingPassword') : t('resetPassword')}
      </Button>
    </form>
  )
}
