import { Eye, EyeOff } from 'lucide-react'
import { useState } from 'react'
import type { UseFormRegisterReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { translateClientFieldError } from '@/shared/api/problems'

export function TextField({
  id,
  label,
  registration,
  error,
  type = 'text',
  autoComplete,
  inputMode,
}: {
  id: string
  label: string
  registration: UseFormRegisterReturn
  error?: { message?: string }
  type?: 'text' | 'email'
  autoComplete?: string
  inputMode?: 'email' | 'text'
}) {
  const { t } = useTranslation('problems')
  const errorId = `${id}-error`
  const errorMessage = translateClientFieldError(error, t)
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type={type}
        autoComplete={autoComplete}
        inputMode={inputMode}
        aria-invalid={Boolean(error)}
        aria-describedby={errorMessage ? errorId : undefined}
        {...registration}
      />
      <FieldError id={errorId} errors={errorMessage ? [{ message: errorMessage }] : undefined} />
    </Field>
  )
}

export function PasswordField({
  id,
  label,
  registration,
  error,
  autoComplete,
}: {
  id: string
  label: string
  registration: UseFormRegisterReturn
  error?: { message?: string }
  autoComplete: 'new-password' | 'current-password'
}) {
  const { t: authT } = useTranslation('auth')
  const { t: problemsT } = useTranslation('problems')
  const [visible, setVisible] = useState(false)
  const errorId = `${id}-error`
  const errorMessage = translateClientFieldError(error, problemsT)
  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <div className="relative">
        <Input
          id={id}
          type={visible ? 'text' : 'password'}
          autoComplete={autoComplete}
          aria-invalid={Boolean(error)}
          aria-describedby={errorMessage ? errorId : undefined}
          className="pr-12"
          {...registration}
        />
        <Button
          type="button"
          variant="ghost"
          size="icon"
          className="absolute right-1 top-1/2 -translate-y-1/2"
          aria-label={visible ? authT('hidePassword') : authT('showPassword')}
          onClick={() => setVisible((current) => !current)}
        >
          {visible ? <EyeOff aria-hidden="true" data-icon="inline-start" /> : <Eye aria-hidden="true" data-icon="inline-start" />}
        </Button>
      </div>
      <FieldError id={errorId} errors={errorMessage ? [{ message: errorMessage }] : undefined} />
    </Field>
  )
}
