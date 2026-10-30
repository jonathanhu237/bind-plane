import { useState } from 'react'
import { FieldError } from '@/components/ui/field'

/** A field the form cannot be saved without; `choose` marks a select. */
export type RequiredField = { id: string; label: string; value: string; choose?: boolean }

/** Names the empty field by its label, without hints such as （可选）. */
export function requiredMessage(field: Pick<RequiredField, 'label' | 'choose'>, zh: boolean): string {
  const label = field.label.replace(/（[^）]*）|\([^)]*\)/g, '').trim()
  if (!zh) return `${label} is required.`
  return `${field.choose ? '请选择' : '请输入'}${/^[A-Za-z0-9]/.test(label) ? ' ' : ''}${label}。`
}

/**
 * Required fields checked in the interface language once the form is first submitted;
 * each message goes away as its field is filled. Forms set noValidate, because the
 * browser's own bubble speaks the browser's language, not the interface's.
 */
export function useRequiredFields(zh: boolean, fields: RequiredField[]) {
  const [checked, setChecked] = useState(false)
  const missing = new Map(checked ? fields.filter((field) => !field.value.trim()).map((field) => [field.id, field]) : [])
  return {
    /** Call first when saving; false means a field is empty, and the first empty one has focus. */
    check(): boolean {
      setChecked(true)
      const first = fields.find((field) => !field.value.trim())
      if (first) document.getElementById(first.id)?.focus()
      return !first
    },
    /** Forget earlier attempts, for example after the form was saved and cleared. */
    reset() {
      setChecked(false)
    },
    /** For the Field's data-invalid. */
    invalid: (id: string) => missing.has(id),
    /** ARIA for the control; `describedBy` keeps a hint the control already points to. */
    control(id: string, describedBy?: string) {
      return missing.has(id)
        ? { 'aria-invalid': true, 'aria-describedby': [describedBy, `${id}-required`].filter(Boolean).join(' ') }
        : { 'aria-describedby': describedBy }
    },
    message(id: string) {
      const field = missing.get(id)
      return field ? <FieldError id={`${id}-required`}>{requiredMessage(field, zh)}</FieldError> : null
    },
  }
}
