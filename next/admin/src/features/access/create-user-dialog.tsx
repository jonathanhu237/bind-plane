import { useMutation } from '@tanstack/react-query'
import { Copy, KeyRound, UserPlus } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { DialogClose, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import type { ApiClient } from '@/shared/api/client'
import type { AccessUser, Role } from '@/shared/api/contracts'
import { fieldProblemFor } from '@/shared/api/problems'
import { notifyFailure, notifyRequestError, notifySuccess } from '@/shared/feedback'
import { copyText } from '@/lib/clipboard'
import { passwordMeetsRules } from '@/features/auth/schemas'
import { generatePassword } from './password-generator'

type Problem = 'name' | 'email' | 'password' | 'roles' | undefined

/**
 * Creates an account directly, with an initial password the administrator hands over. The
 * password lives only in this dialog's state and is shown once, after the account is created.
 */
export function CreateUserForm({ api, roles, assignableRoleIDs, onCreated, onDone }: { api: ApiClient; roles: Role[]; assignableRoleIDs?: Set<string>; onCreated: () => void; onDone: () => void }) {
  const { t } = useTranslation(['access', 'problems', 'common'])
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState(() => generatePassword())
  const [roleIDs, setRoleIDs] = useState<string[]>([])
  const [requireChange, setRequireChange] = useState(true)
  const [problem, setProblem] = useState<Problem>()
  const [created, setCreated] = useState<{ user: AccessUser; password: string }>()
  const mutation = useMutation({
    retry: false,
    mutationFn: async () => {
      if (!api.createUser) throw new Error('missing createUser')
      return api.createUser({ name: name.trim(), email: email.trim(), password, roleIds: roleIDs, requirePasswordChange: requireChange })
    },
    onSuccess: ({ user }) => { setCreated({ user, password }); onCreated() },
    onError: (error) => {
      for (const field of ['name', 'email', 'password', 'roleIds'] as const) {
        if (fieldProblemFor(error, `/${field}`)) { setProblem(field === 'roleIds' ? 'roles' : field); break }
      }
      notifyRequestError(error, t, { title: t('createUser') })
    },
  })
  const copy = async (text: string, button: HTMLElement) => {
    const copied = await copyText(text, button.closest<HTMLElement>('[role="dialog"]') ?? undefined)
    if (copied) notifySuccess(t('copied'))
    else notifyFailure(t('copyFailed'))
  }
  const submit = () => {
    const check: Problem = !name.trim() ? 'name' : !/^[^\s@]+@[^\s@]+$/.test(email.trim()) ? 'email' : roleIDs.length === 0 ? 'roles' : !passwordMeetsRules(password) ? 'password' : undefined
    setProblem(check)
    if (!check && !mutation.isPending) mutation.mutate()
  }

  if (created) {
    return <>
      <DialogHeader><DialogTitle>{t('userCreatedTitle')}</DialogTitle><DialogDescription>{t('userCreatedDescription')}</DialogDescription></DialogHeader>
      <dl className="grid gap-3 rounded-md border p-4 text-sm">
        <div className="flex items-center justify-between gap-3"><div className="min-w-0"><dt className="text-muted-foreground">{t('inviteEmail')}</dt><dd className="truncate font-medium">{created.user.email}</dd></div><Button type="button" variant="outline" size="sm" onClick={(event) => void copy(created.user.email, event.currentTarget)}><Copy aria-hidden="true" data-icon="inline-start" />{t('copy')}</Button></div>
        <div className="flex items-center justify-between gap-3"><div className="min-w-0"><dt className="text-muted-foreground">{t('initialPassword')}</dt><dd className="break-all font-mono">{created.password}</dd></div><Button type="button" variant="outline" size="sm" onClick={(event) => void copy(created.password, event.currentTarget)}><Copy aria-hidden="true" data-icon="inline-start" />{t('copy')}</Button></div>
      </dl>
      {created.user.passwordChangeRequired ? <p className="text-sm text-muted-foreground">{t('userCreatedChangeNote')}</p> : null}
      <DialogFooter><Button type="button" onClick={onDone}>{t('done')}</Button></DialogFooter>
    </>
  }
  const busy = mutation.isPending
  return <>
    <DialogHeader><DialogTitle>{t('createUser')}</DialogTitle><DialogDescription>{t('createUserDescription')}</DialogDescription></DialogHeader>
    <form className="flex flex-col gap-5" noValidate onSubmit={(event) => { event.preventDefault(); submit() }}>
      <FieldGroup>
        <Field data-invalid={problem === 'name'}><FieldLabel htmlFor="create-user-name">{t('inviteName')}</FieldLabel><Input id="create-user-name" value={name} onChange={(event) => { setName(event.target.value); setProblem(undefined) }} aria-invalid={problem === 'name'} autoComplete="off" disabled={busy} />{problem === 'name' ? <FieldError>{t('problems:fields.invalidName')}</FieldError> : null}</Field>
        <Field data-invalid={problem === 'email'}><FieldLabel htmlFor="create-user-email">{t('inviteEmail')}</FieldLabel><Input id="create-user-email" type="email" value={email} onChange={(event) => { setEmail(event.target.value); setProblem(undefined) }} aria-invalid={problem === 'email'} autoComplete="off" disabled={busy} />{problem === 'email' ? <FieldError>{t('problems:fields.invalidEmail')}</FieldError> : null}</Field>
        <Field data-invalid={problem === 'password'}>
          <FieldLabel htmlFor="create-user-password">{t('initialPassword')}</FieldLabel>
          <div className="flex gap-2"><Input id="create-user-password" className="font-mono" value={password} onChange={(event) => { setPassword(event.target.value); setProblem(undefined) }} aria-invalid={problem === 'password'} autoComplete="off" spellCheck={false} disabled={busy} /><Button type="button" variant="outline" onClick={() => { setPassword(generatePassword()); setProblem(undefined) }} disabled={busy}><KeyRound aria-hidden="true" data-icon="inline-start" />{t('generatePassword')}</Button></div>
          {problem === 'password' ? <FieldError>{t('problems:fields.invalidPassword')}</FieldError> : <FieldDescription>{t('problems:fields.invalidPassword')}</FieldDescription>}
        </Field>
        <fieldset className="flex flex-col gap-2" aria-describedby={problem === 'roles' ? 'create-user-roles-error' : undefined}>
          <legend className="text-sm font-medium">{t('role')}</legend>
          {roles.map((role) => {
            const allowed = !assignableRoleIDs || assignableRoleIDs.has(role.id)
            return <label key={role.id} className="flex items-center gap-3 rounded-md border p-3 text-sm"><Checkbox checked={roleIDs.includes(role.id)} onCheckedChange={(value) => { setRoleIDs(value === true ? [...roleIDs, role.id] : roleIDs.filter((id) => id !== role.id)); setProblem(undefined) }} aria-label={role.name} disabled={!allowed || busy} />{role.name}{!allowed ? <span className="ml-auto text-xs text-muted-foreground">{t('unavailable')}</span> : null}</label>
          })}
          {problem === 'roles' ? <FieldError id="create-user-roles-error">{t('requiredRole')}</FieldError> : null}
        </fieldset>
        <Field orientation="horizontal"><Checkbox id="create-user-require-change" checked={requireChange} onCheckedChange={(value) => setRequireChange(value === true)} disabled={busy} /><div className="flex flex-col gap-1"><FieldLabel htmlFor="create-user-require-change" className="font-normal">{t('requirePasswordChange')}</FieldLabel><FieldDescription>{t('requirePasswordChangeHint')}</FieldDescription></div></Field>
      </FieldGroup>
      <DialogFooter><DialogClose asChild><Button type="button" variant="outline" disabled={busy}>{t('common:cancel')}</Button></DialogClose><Button type="submit" disabled={busy}><UserPlus aria-hidden="true" data-icon="inline-start" />{busy ? t('creatingAccount') : t('createAccount')}</Button></DialogFooter>
    </form>
  </>
}
