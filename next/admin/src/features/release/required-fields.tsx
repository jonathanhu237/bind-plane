import { useState } from 'react'
import { FieldError } from '@/components/ui/field'

type Control = HTMLInputElement | HTMLSelectElement | HTMLTextAreaElement

const isControl = (element: Element): element is Control =>
  element instanceof HTMLInputElement || element instanceof HTMLSelectElement || element instanceof HTMLTextAreaElement

/** Names the empty field by its label, without hints such as （可选）. */
export function requiredMessage(control: Control, zh: boolean): string {
  const choose = control instanceof HTMLSelectElement
  if (!zh) return choose ? 'Choose an option.' : 'This field is required.'
  const label = (control.labels?.[0]?.textContent ?? '').replace(/（[^）]*）|\([^)]*\)/g, '').trim()
  if (!label) return choose ? '请选择一项。' : '请填写此项。'
  return `${choose ? '请选择' : '请输入'}${/^[A-Za-z0-9]/.test(label) ? ' ' : ''}${label}。`
}

/**
 * Required fields checked in the interface language. Controls keep `required`, which assistive
 * technology announces, and the form sets noValidate: the browser's own bubble speaks the
 * browser's language, not the interface's.
 */
export function useRequiredFields(zh: boolean) {
  const [problems, setProblems] = useState<Record<string, string>>({})
  return {
    /** Call first in onSubmit; false means a required field is empty, and it now has focus. */
    check(form: HTMLFormElement): boolean {
      const empty = Array.from(form.elements).filter(isControl).filter((control) => control.required && !control.disabled && !control.value.trim())
      setProblems(Object.fromEntries(empty.map((control) => [control.id, requiredMessage(control, zh)])))
      empty[0]?.focus()
      return empty.length === 0
    },
    /** The form's onChange: editing a field clears its message. */
    onChange(event: React.FormEvent<HTMLFormElement>) {
      const { id } = event.target as HTMLElement
      if (problems[id]) setProblems((current) => Object.fromEntries(Object.entries(current).filter(([key]) => key !== id)))
    },
    /** ARIA for one control; `describedBy` keeps a hint the control already points to. */
    invalid(id: string, describedBy?: string) {
      return problems[id]
        ? { 'aria-invalid': true, 'aria-describedby': [describedBy, `${id}-required`].filter(Boolean).join(' ') }
        : { 'aria-describedby': describedBy }
    },
    message(id: string) {
      return problems[id] ? <FieldError id={`${id}-required`}>{problems[id]}</FieldError> : null
    },
  }
}
