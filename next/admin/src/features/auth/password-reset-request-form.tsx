import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { fieldProblemFor, translatePasswordResetProblem } from '@/shared/api/problems'
import { normalizePasswordResetRequestValues, passwordResetRequestFormSchema, type PasswordResetRequestFormValues } from './schemas'
import { TextField } from './form-fields'

export function PasswordResetRequestForm({ api, onAccepted }: { api: ApiClient; onAccepted: () => void }) {
  const { t } = useTranslation(['auth', 'problems'])
  const [formError, setFormError] = useState<unknown>()
  const form = useForm<PasswordResetRequestFormValues>({
    resolver: zodResolver(passwordResetRequestFormSchema),
    mode: 'onBlur',
    shouldFocusError: true,
    defaultValues: { email: '' },
  })
  const mutation = useMutation({
    retry: false,
    mutationFn: (values: PasswordResetRequestFormValues) => api.requestPasswordReset(normalizePasswordResetRequestValues(values)),
    onSuccess: onAccepted,
  })

  const submit = form.handleSubmit(async (values) => {
    setFormError(undefined)
    try {
      await mutation.mutateAsync(values)
    } catch (error) {
      if (error instanceof ApiProblemError) {
        const field = fieldProblemFor(error, '/email')
        if (field) form.setError('email', { type: 'server', message: field.code }, { shouldFocus: true })
      }
      setFormError(error)
    }
  })

  return (
    <form noValidate onSubmit={(event) => void submit(event)} className="flex flex-col gap-6">
      {formError !== undefined ? <Alert variant="destructive" role="alert" aria-live="polite"><AlertDescription>{translatePasswordResetProblem(formError, t)}</AlertDescription></Alert> : null}
      <FieldGroup className="gap-5">
        <TextField id="email" label={t('emailLabel')} registration={form.register('email')} error={form.formState.errors.email} type="email" inputMode="email" autoComplete="email" />
      </FieldGroup>
      <Button type="submit" className="w-full" disabled={mutation.isPending}>
        {mutation.isPending ? t('sendingResetLink') : t('sendResetLink')}
      </Button>
    </form>
  )
}
