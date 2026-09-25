import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { fieldProblemFor, isInvalidSetupToken, isSetupComplete, translateRateLimitedProblem } from '@/shared/api/problems'
import { clearSetupAuthority } from '@/shared/bootstrap/setup-authority'
import { setupFormSchema, normalizeSetupValues, type SetupFormValues } from './schemas'
import { PasswordField, TextField } from './form-fields'
import { i18n } from '@/shared/i18n'

export function SetupForm({ api, token, onSuccess, onInvalidAuthority, onSetupComplete }: { api: ApiClient; token: string; onSuccess: () => void; onInvalidAuthority?: () => void; onSetupComplete?: () => void }) {
  const { t } = useTranslation(['auth', 'problems'])
  const queryClient = useQueryClient()
  const [formError, setFormError] = useState<unknown>()
  const form = useForm<SetupFormValues>({
    resolver: zodResolver(setupFormSchema),
    mode: 'onBlur',
    shouldFocusError: true,
    defaultValues: { name: '', email: '', password: '', passwordConfirmation: '' },
  })
  const mutation = useMutation({
    retry: false,
    mutationFn: (values: SetupFormValues) => api.setup({ token, ...normalizeSetupValues(values), locale: i18n.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en' }),
    onSuccess: () => {
      clearSetupAuthority()
      void queryClient.invalidateQueries({ queryKey: ['setup', 'status'] })
      onSuccess()
    },
  })

  const submit = form.handleSubmit(async (values) => {
    setFormError(undefined)
    try {
      await mutation.mutateAsync(values)
    } catch (error) {
      let focused = false
      if (error instanceof ApiProblemError) {
        if (isInvalidSetupToken(error)) {
          clearSetupAuthority()
          onInvalidAuthority?.()
        }
        if (isSetupComplete(error)) {
          clearSetupAuthority()
          onSetupComplete?.()
          return
        }
        for (const fieldName of ['name', 'email', 'password', 'passwordConfirmation'] as const) {
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
      {formError !== undefined ? <Alert variant="destructive" role="alert" aria-live="polite"><AlertDescription>{translateRateLimitedProblem(formError, t, 'setupRateLimited')}</AlertDescription></Alert> : null}
      <FieldGroup className="gap-5">
        <TextField id="name" label={t('nameLabel')} registration={form.register('name')} error={form.formState.errors.name} autoComplete="name" />
        <TextField id="email" label={t('emailLabel')} registration={form.register('email')} error={form.formState.errors.email} type="email" inputMode="email" autoComplete="email" />
        <PasswordField id="password" label={t('passwordLabel')} registration={form.register('password')} error={form.formState.errors.password} autoComplete="new-password" />
        <PasswordField id="passwordConfirmation" label={t('confirmPasswordLabel')} registration={form.register('passwordConfirmation')} error={form.formState.errors.passwordConfirmation} autoComplete="new-password" />
      </FieldGroup>
      <Button type="submit" className="w-full" disabled={mutation.isPending}>
        {mutation.isPending ? t('initializing') : t('initialize')}
      </Button>
    </form>
  )
}
