import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { FieldGroup } from '@/components/ui/field'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { fieldProblemFor, translateRateLimitedProblem } from '@/shared/api/problems'
import { currentUserQueryKey } from './queries'
import { changeAccountLocale, i18n } from '@/shared/i18n'
import { loginFormSchema, normalizeLoginValues, type LoginFormValues } from './schemas'
import { PasswordField, TextField } from './form-fields'

export function LoginForm({ api, onSuccess }: { api: ApiClient; onSuccess: () => void }) {
  const { t } = useTranslation(['auth', 'problems'])
  const queryClient = useQueryClient()
  const [formError, setFormError] = useState<unknown>()
  const form = useForm<LoginFormValues>({
    resolver: zodResolver(loginFormSchema),
    mode: 'onSubmit',
    shouldFocusError: true,
    defaultValues: { email: '', password: '' },
  })
  const mutation = useMutation({
    retry: false,
    mutationFn: (values: LoginFormValues) => api.login({ ...normalizeLoginValues(values), locale: i18n.language.toLowerCase().startsWith('zh') ? 'zh-CN' : 'en' }),
    onSuccess: (user) => {
      queryClient.setQueryData(currentUserQueryKey, user)
      if (user.locale === 'en' || user.locale === 'zh-CN') void changeAccountLocale(user.locale)
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
        for (const fieldName of ['email', 'password'] as const) {
          const field = fieldProblemFor(error, `/${fieldName}`)
          if (field) {
            const message = fieldName === 'password' && field.code === 'invalid_login_password'
              ? (normalizeLoginValues(values).password.length === 0 ? 'login_password_required' : 'login_password_too_long')
              : field.code
            form.setError(fieldName, { type: 'server', message }, { shouldFocus: !focused })
            focused = true
          }
        }
      }
      if (!focused) setFormError(error)
    }
  })

  return (
    <form noValidate onSubmit={(event) => { setFormError(undefined); void submit(event) }} className="flex flex-col gap-6">
      {formError !== undefined ? <Alert variant="destructive" role="alert" aria-live="polite"><AlertDescription>{translateRateLimitedProblem(formError, t, 'rateLimited')}</AlertDescription></Alert> : null}
      <FieldGroup className="gap-5">
        <TextField id="email" label={t('emailLabel')} registration={form.register('email')} error={form.formState.errors.email} type="email" inputMode="email" autoComplete="username" />
        <PasswordField id="password" label={t('passwordLabel')} registration={form.register('password')} error={form.formState.errors.password} autoComplete="current-password" />
      </FieldGroup>
      <Button type="submit" className="w-full" disabled={mutation.isPending}>
        {mutation.isPending ? t('loggingIn') : t('login')}
      </Button>
      <a href="/forgot-password" className="text-center text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground">
        {t('forgotPassword')}
      </a>
    </form>
  )
}
