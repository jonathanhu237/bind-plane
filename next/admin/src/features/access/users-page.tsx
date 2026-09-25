import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef, OnChangeFn, SortingState } from '@tanstack/react-table'
import { Mail, MoreHorizontal, Pencil, Save, UserRoundCheck, UserRoundX, Trash2 } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuGroup, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Badge } from '@/components/ui/badge'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Card, CardContent } from '@/components/ui/card'
import { DialogClose, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Dialog } from '@/components/ui/dialog'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import type { Role } from '@/shared/api/contracts'
import { isForbidden, translateRateLimitedProblemWithFields } from '@/shared/api/problems'
import { DataTable, SortableHeader } from './data-table'
import { nextDraftSubmissionID, useAccessDraftStore } from './drafts'
import { PageNavigation, RoleBadges, canAssignRole, formatDate, type AccessUser } from './access-components'
import { usersOptions } from './queries'
import { notifyRequestError, notifySuccess, readFailureFeedback, useRequestErrorToast } from '@/shared/feedback'
import { UserAvatar } from '@/features/auth/user-avatar'

export function UsersPage({ api, canManage, actorPermissions, actorSuperAdmin = false, actorID }: { api: ApiClient; canManage: boolean; actorPermissions?: string[]; actorSuperAdmin?: boolean; actorID?: string }) {
  const canAssign = canManage && (actorSuperAdmin || actorPermissions === undefined || actorPermissions.includes('roles.read'))
  const { t, i18n } = useTranslation(['access', 'common', 'problems'])
  const queryClient = useQueryClient()
  const [userCursor, setUserCursor] = useState('')
  const [userHistory, setUserHistory] = useState<string[]>([])
  const [userSearch, setUserSearch] = useState('')
  const [userStatus, setUserStatus] = useState<'active' | 'disabled' | ''>('')
  const [userSort, setUserSort] = useState<'name' | 'email' | 'roles' | 'createdAt'>('createdAt')
  const [userDirection, setUserDirection] = useState<'asc' | 'desc'>('desc')
  const users = useQuery(usersOptions(api, { cursor: userCursor, q: userSearch, status: userStatus || undefined, sort: userSort, direction: userDirection }))
  const roleAdministration = useQuery({
    queryKey: ['access', 'roles'],
    queryFn: ({ signal }) => api.getRoles ? api.getRoles(signal) : Promise.reject(new Error('missing getRoles')),
    retry: false,
    enabled: canAssign,
    staleTime: 10_000,
  })
  useRequestErrorToast(users.error, users.isError, t, readFailureFeedback(users.error, { unavailableTitle: t('unavailableTitle'), unavailableDescription: t('unavailableDescription'), forbiddenTitle: t('forbiddenTitle'), forbiddenDescription: t('forbiddenDescription') }))
  useRequestErrorToast(roleAdministration.error, roleAdministration.isError && canAssign, t, readFailureFeedback(roleAdministration.error, { unavailableTitle: t('unavailableTitle'), unavailableDescription: t('unavailableDescription'), forbiddenTitle: t('forbiddenTitle'), forbiddenDescription: t('forbiddenDescription') }))
  const [assignmentOpen, setAssignmentOpen] = useState(false)
  const [deleteEmail, setDeleteEmail] = useState('')
  const [lifecycleTarget, setLifecycleTarget] = useState<{ user: AccessUser; action: 'deactivate' | 'reactivate' | 'delete' }>()
  const [assignmentUser, setAssignmentUser] = useState<AccessUser | undefined>()
  const [assignmentGeneration, setAssignmentGeneration] = useState(0)
  const assignmentGenerationRef = useRef(0)

  const resetPaging = (value: string) => { setUserSearch(value); setUserCursor(''); setUserHistory([]) }
  const handleSorting: OnChangeFn<SortingState> = (updater) => {
    const next = typeof updater === 'function' ? updater([{ id: userSort, desc: userDirection === 'desc' }]) : updater
    const first = next[0]
    setUserSort((first?.id as 'name' | 'email' | 'roles' | 'createdAt' | undefined) ?? 'createdAt')
    setUserDirection(first?.desc ? 'desc' : 'asc')
    setUserCursor(''); setUserHistory([])
  }
  const usersForbidden = users.isError && isForbidden(users.error)
  const roleAdministrationForbidden = canAssign && roleAdministration.isError && isForbidden(roleAdministration.error)
  const roleList = roleAdministrationForbidden ? [] : roleAdministration.data?.roles ?? []
  const activeAssignmentUser = assignmentUser ? users.data?.users.find((item) => item.id === assignmentUser.id) ?? assignmentUser : undefined
  const openAssignment = (user: AccessUser) => {
    const next = assignmentGenerationRef.current + 1
    assignmentGenerationRef.current = next
    setAssignmentGeneration(next)
    setAssignmentUser(user)
    setAssignmentOpen(true)
  }

  const lifecycle = useMutation({
    mutationFn: async ({ user, action }: { user: AccessUser; action: 'deactivate' | 'reactivate' | 'delete' }) => {
      const mutate = action === 'deactivate' ? api.deactivateUser : action === 'reactivate' ? api.reactivateUser : api.deleteUser
      if (!mutate) throw new Error('missing user lifecycle API')
      return mutate(user.id, user.authVersion)
    },
    onSuccess: () => {
      notifySuccess(t('lifecycleSaved'))
      setLifecycleTarget(undefined)
      setDeleteEmail('')
      void queryClient.invalidateQueries({ queryKey: ['access'] })
      void queryClient.invalidateQueries({ queryKey: ['online-users'] })
    },
    onError: (error) => {
      notifyRequestError(error, t, { title: t('actions') })
      if (error instanceof ApiProblemError && error.problem.status === 409) { setLifecycleTarget(undefined); setDeleteEmail(''); void queryClient.invalidateQueries({ queryKey: ['access', 'users'] }) }
    },
  })
  const userColumns = useMemo<ColumnDef<AccessUser, unknown>[]>(() => [
    {
      accessorKey: 'name',
      header: ({ column }) => <SortableHeader column={column}>{t('inviteName')}</SortableHeader>,
      cell: ({ row }) => <div className="flex min-w-0 items-center gap-2"><UserAvatar user={row.original} className="size-7 rounded-md" /><span className="max-w-56 truncate font-medium">{row.original.name}</span></div>,
    },
    {
      accessorKey: 'email',
      header: ({ column }) => <SortableHeader column={column}>{t('inviteEmail')}</SortableHeader>,
      cell: ({ row }) => <span className="text-muted-foreground">{row.original.email}</span>,
    },
    {
      id: 'status', header: t('status'), enableSorting: false,
      cell: ({ row }) => <Badge variant={row.original.disabled ? 'secondary' : 'outline'}>{t(row.original.disabled ? 'disabledStatus' : 'activeStatus')}</Badge>,
    },
    {
      id: 'roles',
      accessorFn: (user) => user.roles.map((role) => role.name).join(', '),
      header: ({ column }) => <SortableHeader column={column}>{t('role')}</SortableHeader>,
      cell: ({ row }) => <RoleBadges roles={row.original.roles} />,
    },
    {
      accessorKey: 'createdAt',
      header: ({ column }) => <SortableHeader column={column}>{t('createdAt')}</SortableHeader>,
      cell: ({ row }) => <time dateTime={row.original.createdAt}>{formatDate(row.original.createdAt, i18n.language)}</time>,
    },
    {
      id: 'actions',
      enableSorting: false,
      header: () => <div className="text-center">{t('actions')}</div>,
      cell: ({ row }) => canManage ? <div className="flex justify-center">
        <DropdownMenu>
          <DropdownMenuTrigger asChild><Button type="button" variant="ghost" size="icon" aria-label={t('moreActions')} disabled={lifecycle.isPending || users.isError}><MoreHorizontal aria-hidden="true" /></Button></DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuGroup>
              {canAssign && !roleAdministrationForbidden ? <DropdownMenuItem onSelect={() => openAssignment(row.original)}><Pencil aria-hidden="true" />{t('assignRoles')}</DropdownMenuItem> : null}
              <DropdownMenuItem disabled={row.original.id === actorID} title={row.original.id === actorID ? t('problems:selfUserOperation') : undefined} onSelect={() => { setDeleteEmail(''); setLifecycleTarget({ user: row.original, action: row.original.disabled ? 'reactivate' : 'deactivate' }) }}>{row.original.disabled ? <UserRoundCheck aria-hidden="true" /> : <UserRoundX aria-hidden="true" />}{t(row.original.disabled ? 'reactivate' : 'deactivate')}</DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem disabled={row.original.id === actorID} title={row.original.id === actorID ? t('problems:selfUserOperation') : undefined} onSelect={() => { setDeleteEmail(''); setLifecycleTarget({ user: row.original, action: 'delete' }) }}><Trash2 aria-hidden="true" />{t('deleteUser')}</DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div> : null,
    },
  ], [canManage, canAssign, actorID, users.isError, i18n.language, lifecycle.isPending, roleAdministrationForbidden, t])

  if (users.isPending || (canAssign && roleAdministration.isPending)) return <p role="status">{t('common:loading')}</p>
  return <section className="flex flex-col gap-5" aria-labelledby="users-title">
    <h1 id="users-title" className="text-2xl font-semibold tracking-tight">{t('usersTitle')}</h1>
    <Card>
      <CardContent className="pt-6">
        {users.isError && (usersForbidden || !users.data) ? <p role="status" className="text-sm text-muted-foreground">{usersForbidden ? t('forbiddenDescription') : t('common:refreshPage')}</p> : <>
        <Field className="mb-4 max-w-xs"><FieldLabel htmlFor="user-status">{t('filterByStatus')}</FieldLabel><Select value={userStatus || 'all'} onValueChange={(value) => { setUserStatus(value === 'all' ? '' : value as 'active' | 'disabled'); setUserCursor(''); setUserHistory([]) }}><SelectTrigger id="user-status"><SelectValue /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="all">{t('allStatuses')}</SelectItem><SelectItem value="active">{t('activeStatus')}</SelectItem><SelectItem value="disabled">{t('disabledStatus')}</SelectItem></SelectGroup></SelectContent></Select></Field>
        <DataTable
          columns={userColumns}
          data={users.data?.users ?? []}
          search={userSearch}
          onSearchChange={resetPaging}
          searchPlaceholder={t('searchUsers')}
          clearSearchLabel={t('clearSearch')}
          emptyMessage={userSearch ? t('noSearchResults') : t('noUsers')}
          sorting={[{ id: userSort, desc: userDirection === 'desc' }]}
          onSortingChange={handleSorting}
          manualFiltering
          manualSorting
        />
        {users.isFetching && !users.isPending ? <p role="status" className="mt-3 text-sm text-muted-foreground">{t('common:loading')}</p> : null}
        <PageNavigation hasPrevious={userHistory.length > 0} hasNext={Boolean(users.data?.nextCursor)} loading={users.isFetching} onPrevious={() => { const previous = userHistory[userHistory.length - 1] ?? ''; setUserHistory((current) => current.slice(0, -1)); setUserCursor(previous) }} onNext={() => { if (!users.data?.nextCursor) return; setUserHistory((current) => [...current, userCursor]); setUserCursor(users.data.nextCursor) }} t={(key) => t(key as never)} />
        </>}
      </CardContent>
    </Card>
    <Dialog open={Boolean(lifecycleTarget)} onOpenChange={(open) => { if (!open && !lifecycle.isPending) { setLifecycleTarget(undefined); setDeleteEmail('') } }}>
      <DialogContent>
        <DialogHeader><DialogTitle>{lifecycleTarget?.action === 'delete' ? t('deleteUser') : lifecycleTarget?.action === 'deactivate' ? t('deactivate') : t('reactivate')}</DialogTitle><DialogDescription>{lifecycleTarget ? (lifecycleTarget.action === 'delete' ? t('deleteWarning') : lifecycleTarget.action === 'deactivate' ? t('confirmDeactivate', { name: lifecycleTarget.user.name }) : t('confirmReactivate', { name: lifecycleTarget.user.name })) : ''}</DialogDescription></DialogHeader>
        {lifecycleTarget ? <p>{lifecycleTarget.user.name} · {lifecycleTarget.user.email}</p> : null}
        {lifecycleTarget?.action === 'delete' ? <Field><FieldLabel htmlFor="delete-user-email">{t('deletePrompt', { email: lifecycleTarget.user.email })}</FieldLabel><Input id="delete-user-email" value={deleteEmail} disabled={lifecycle.isPending} onChange={(event) => setDeleteEmail(event.target.value)} /></Field> : null}
        <DialogFooter><DialogClose asChild><Button type="button" variant="outline" disabled={lifecycle.isPending}>{t('common:cancel')}</Button></DialogClose><Button type="button" variant={lifecycleTarget?.action === 'delete' ? 'destructive' : 'default'} disabled={lifecycle.isPending || (lifecycleTarget?.action === 'delete' && deleteEmail !== lifecycleTarget.user.email)} onClick={() => { if (lifecycleTarget && !lifecycle.isPending && (lifecycleTarget.action !== 'delete' || deleteEmail === lifecycleTarget.user.email)) lifecycle.mutate(lifecycleTarget) }}>{lifecycleTarget?.action === 'delete' ? t('deleteUser') : lifecycleTarget?.action === 'deactivate' ? t('deactivate') : t('reactivate')}</Button></DialogFooter>
      </DialogContent>
    </Dialog>
    {canAssign && !usersForbidden && !roleAdministrationForbidden ? <UserAssignmentDialog key={activeAssignmentUser?.id ?? 'none'} open={assignmentOpen} user={activeAssignmentUser} roles={roleList} actorPermissions={actorPermissions} actorSuperAdmin={actorSuperAdmin} api={api} canManage={canManage} assignmentGeneration={assignmentGeneration} onClose={() => setAssignmentOpen(false)} onDone={(_userID, generation) => { if (generation !== assignmentGenerationRef.current) return; setAssignmentOpen(false); void queryClient.invalidateQueries({ queryKey: ['access', 'users'] }); void queryClient.invalidateQueries({ queryKey: ['auth', 'current-user'] }) }} /> : null}
  </section>
}

export function UserAssignmentDialog({ api, user, roles, actorPermissions, actorSuperAdmin = false, canManage = true, open, assignmentGeneration, onClose, onDone }: { api: ApiClient; user?: AccessUser; roles: Role[]; actorPermissions?: string[]; actorSuperAdmin?: boolean; canManage?: boolean; open: boolean; assignmentGeneration: number; onClose: () => void; onDone: (userID: string, generation: number) => void }) {
  const { t } = useTranslation(['access', 'problems', 'common'])
  const queryClient = useQueryClient()
  const assignment = useAccessDraftStore((state) => user ? state.assignments[user.id] : undefined)
  const setAssignment = useAccessDraftStore((state) => state.setAssignment)
  const conflictAnnounced = useRef<string | undefined>(undefined)
  const initialRoles = useMemo(() => user?.roles.map((role) => role.id).slice().sort() ?? [], [user])
  useEffect(() => {
    if (!open) return
    if (!user || assignment) return
    setAssignment(user.id, { userID: user.id, authVersion: user.authVersion, roleIDs: initialRoles, submitting: false, conflict: false, validationError: false, invalidRoleSelection: false })
  }, [assignment, initialRoles, open, setAssignment, user])
  useEffect(() => {
    if (!user || !assignment || assignment.authVersion === user.authVersion || assignment.conflict) return
    setAssignment(user.id, { ...assignment, conflict: true })
    const signature = `${user.id}:${assignment.authVersion}:${user.authVersion}`
    if (conflictAnnounced.current !== signature) {
      conflictAnnounced.current = signature
      notifyRequestError(new ApiProblemError({ type: '/problems/stale-revision', title: 'stale revision', status: 409, code: 'stale_revision' }), t, { title: t('conflictTitle'), description: t('draftConflictDescription') })
    }
  }, [assignment, setAssignment, t, user])
  const current = assignment ?? { userID: user?.id ?? '', authVersion: user?.authVersion ?? 0, roleIDs: initialRoles, submitting: false, conflict: false, validationError: false }
  type Submission = { ownerID?: string; userID: string; submissionID: string }
  const updateDraft = (patch: Partial<typeof current>, submission?: Submission): boolean => {
    if (!user) return false
    const state = useAccessDraftStore.getState()
    const existing = state.assignments[user.id]
    if (submission && (state.ownerID !== submission.ownerID || existing?.userID !== submission.userID || existing?.submissionID !== submission.submissionID)) return false
    setAssignment(user.id, { ...(existing ?? current), ...patch })
    return true
  }
  const update = (patch: Partial<typeof current>) => { updateDraft(patch) }
  const resetDraft = () => {
    if (user) setAssignment(user.id, { userID: user.id, authVersion: user.authVersion, roleIDs: initialRoles, submitting: false, conflict: false, validationError: false, invalidRoleSelection: false })
  }
  const beginSubmission = (): Submission => {
    const state = useAccessDraftStore.getState()
    const submission = { ownerID: state.ownerID, userID: user?.id ?? '', submissionID: nextDraftSubmissionID() }
    updateDraft({ ...(state.assignments[user?.id ?? ''] ?? current), submissionID: submission.submissionID, submitting: false, conflict: false })
    return submission
  }
  const mutation = useMutation({
    retry: false,
    mutationFn: async (submission: Submission) => {
      const state = useAccessDraftStore.getState()
      const draft = user ? state.assignments[user.id] : undefined
      if (!user || !draft || state.ownerID !== submission.ownerID || draft.userID !== submission.userID || draft.submissionID !== submission.submissionID) throw new Error('stale draft')
      if (draft.roleIDs.length === 0) { updateDraft({ validationError: true, invalidRoleSelection: false }, submission); throw new Error('invalid roles') }
      const availableRoleIDs = new Set(roles.map((role) => role.id))
      if (draft.roleIDs.some((roleID) => !availableRoleIDs.has(roleID))) {
        updateDraft({ validationError: true, invalidRoleSelection: true }, submission)
        throw new Error('invalid roles')
      }
      if (!updateDraft({ submitting: true, validationError: false, invalidRoleSelection: false, conflict: false }, submission)) throw new Error('stale draft')
      if (!api.replaceUserRoles) throw new Error('missing replaceUserRoles')
      return api.replaceUserRoles(user.id, { roleIds: draft.roleIDs, authVersion: draft.authVersion })
    },
    onSuccess: (_saved, submission) => {
      const state = useAccessDraftStore.getState()
      if (state.ownerID !== submission.ownerID) return
      void queryClient.invalidateQueries({ queryKey: ['access', 'users'] })
      void queryClient.invalidateQueries({ queryKey: ['auth', 'current-user'] })
      const existing = user ? state.assignments[user.id] : undefined
      if (!user || existing?.userID !== submission.userID || existing?.submissionID !== submission.submissionID) return
      setAssignment(user.id, undefined)
      notifySuccess(t('assignmentsSaved'))
      onDone(user.id, assignmentGeneration)
    },
    onError: (error, submission) => {
      if (!submission || !updateDraft({ submitting: false }, submission)) return
      if (isStaleRevision(error)) {
        updateDraft({ conflict: true }, submission)
        notifyRequestError(error, t, { title: t('conflictTitle'), description: t('draftConflictDescription') })
      } else if (!(error instanceof Error && (error.message === 'invalid roles' || error.message === 'stale draft'))) {
        notifyRequestError(error, t, { title: t('saveAssignments') })
      }
    },
  })
  if (!user || !canManage) return null
  const hasChanges = !sameRoleIDs(current.roleIDs, initialRoles)
  return <Dialog open={open} onOpenChange={(value) => { if (!value) onClose() }}>
    <DialogContent forceMount closeLabel={t('common:close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
      <DialogHeader><DialogTitle>{t('assignRoles')}</DialogTitle><DialogDescription>{user.name} · {user.email}</DialogDescription></DialogHeader>
      <fieldset className="flex flex-col gap-2" aria-describedby={current.validationError ? 'assignment-roles-error' : undefined}>
        <legend className="text-sm font-medium">{t('role')}</legend>
        {roles.map((role) => { const allowed = actorPermissions === undefined ? true : canAssignRole(role, actorPermissions, actorSuperAdmin); return <label key={role.id} className="flex items-center gap-3 rounded-md border p-3 text-sm"><Checkbox checked={current.roleIDs.includes(role.id)} onCheckedChange={(value) => update({ roleIDs: value === true ? [...current.roleIDs, role.id] : current.roleIDs.filter((id) => id !== role.id), validationError: false, invalidRoleSelection: false })} aria-label={role.name} disabled={!allowed || current.submitting || mutation.isPending || current.conflict} />{role.name}{!allowed ? <span className="ml-auto text-xs text-muted-foreground">{t('unavailable')}</span> : null}</label> })}
        {current.validationError ? <FieldError id="assignment-roles-error">{current.invalidRoleSelection ? t('invalidRoleSelection') : t('requiredRole')}</FieldError> : null}
      </fieldset>
      <DialogFooter><Button type="button" variant="ghost" onClick={resetDraft} disabled={current.conflict}>{t('common:reset')}</Button><DialogClose asChild><Button type="button" variant="outline">{t('common:cancel')}</Button></DialogClose><Button type="button" disabled={current.submitting || mutation.isPending || current.conflict || current.roleIDs.length === 0 || !hasChanges} onClick={() => { if (!current.submitting && !mutation.isPending && !current.conflict) mutation.mutate(beginSubmission()) }}><Save aria-hidden="true" />{current.submitting || mutation.isPending ? t('saving') : t('saveAssignments')}</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}

function sameRoleIDs(left: string[], right: string[]): boolean {
  if (left.length !== right.length) return false
  const sortedLeft = left.slice().sort()
  const sortedRight = right.slice().sort()
  return sortedLeft.every((id, index) => id === sortedRight[index])
}

function isStaleRevision(error: unknown): boolean {
  return error instanceof ApiProblemError && (error.problem.code === 'stale_revision' || error.problem.type === '/problems/stale-revision')
}

export function InvitationForm({ api, roles, open, onDone, assignableRoleIDs }: { api: ApiClient; roles: Role[]; open: boolean; onDone: () => void; assignableRoleIDs?: Set<string> }) {
  const { t } = useTranslation(['access', 'problems', 'common'])
  const queryClient = useQueryClient()
  const invitation = useAccessDraftStore((state) => state.invitation)
  const setInvitation = useAccessDraftStore((state) => state.setInvitation)
  useEffect(() => {
    if (open && !invitation) setInvitation({ name: '', email: '', roleIDs: [], submitting: false })
  }, [invitation, open, setInvitation])
  const current = invitation ?? { name: '', email: '', roleIDs: [], submitting: false }
  type Submission = { ownerID?: string; submissionID: string }
  const updateDraft = (patch: Partial<typeof current>, submission?: Submission): boolean => {
    const state = useAccessDraftStore.getState()
    const existing = state.invitation
    if (submission && (state.ownerID !== submission.ownerID || existing?.submissionID !== submission.submissionID)) return false
    setInvitation({ ...(existing ?? current), ...patch })
    return true
  }
  const update = (patch: Partial<typeof current>) => { updateDraft(patch) }
  const resetDraft = () => { setInvitation({ name: '', email: '', roleIDs: [], submitting: false }) }
  const beginSubmission = (): Submission => {
    const state = useAccessDraftStore.getState()
    const submission = { ownerID: state.ownerID, submissionID: nextDraftSubmissionID() }
    updateDraft({ ...(state.invitation ?? current), submissionID: submission.submissionID, submitting: false })
    return submission
  }
  const mutation = useMutation({
    retry: false,
    mutationFn: async (submission: Submission) => {
      const state = useAccessDraftStore.getState()
      const draft = state.invitation
      if (!draft || state.ownerID !== submission.ownerID || draft.submissionID !== submission.submissionID) throw new Error('stale draft')
      if (!draft.name.trim()) { updateDraft({ validationError: 'name' }, submission); throw new Error('validation') }
      if (!draft.email.trim()) { updateDraft({ validationError: 'email' }, submission); throw new Error('validation') }
      if (draft.roleIDs.length === 0) { updateDraft({ validationError: 'roles' }, submission); throw new Error('validation') }
      const availableRoleIDs = new Set(roles.filter((role) => !assignableRoleIDs || assignableRoleIDs.has(role.id)).map((role) => role.id))
      if (draft.roleIDs.some((roleID) => !availableRoleIDs.has(roleID))) { updateDraft({ validationError: 'invalidRoles' }, submission); throw new Error('validation') }
      if (!updateDraft({ submitting: true, validationError: undefined }, submission)) throw new Error('stale draft')
      if (!api.createInvitation) throw new Error('missing createInvitation')
      return api.createInvitation({ name: draft.name, email: draft.email, roleIds: draft.roleIDs })
    },
    onSuccess: (_saved, submission) => {
      const state = useAccessDraftStore.getState()
      if (state.ownerID !== submission.ownerID) return
      void queryClient.invalidateQueries({ queryKey: ['access', 'invitations'] })
      if (state.invitation?.submissionID !== submission.submissionID) return
      setInvitation(undefined)
      notifySuccess(t('invitationCreated'))
      onDone()
    },
    onError: (error, submission) => {
      if (!submission || !updateDraft({ submitting: false }, submission)) return
      if (!(error instanceof Error && (error.message === 'validation' || error.message === 'stale draft'))) notifyRequestError(error, t, { title: t('sendInvitation'), description: translateRateLimitedProblemWithFields(error, t, 'invitationSendRateLimited') })
    },
  })
  return <>
    <DialogHeader><DialogTitle>{t('inviteUser')}</DialogTitle></DialogHeader>
    <form className="flex flex-col gap-5" noValidate onSubmit={(event) => { event.preventDefault(); if (!current.submitting && !mutation.isPending) mutation.mutate(beginSubmission()) }}>
      <FieldGroup>
        <Field data-invalid={current.validationError === 'name'}><FieldLabel htmlFor="invite-name">{t('inviteName')}</FieldLabel><Input id="invite-name" value={current.name} onChange={(event) => update({ name: event.target.value, validationError: undefined })} aria-invalid={current.validationError === 'name'} autoComplete="name" disabled={current.submitting || mutation.isPending} />{current.validationError === 'name' ? <FieldError>{t('problems:fields.invalidName')}</FieldError> : null}</Field>
        <Field data-invalid={current.validationError === 'email'}><FieldLabel htmlFor="invite-email">{t('inviteEmail')}</FieldLabel><Input id="invite-email" value={current.email} onChange={(event) => update({ email: event.target.value, validationError: undefined })} aria-invalid={current.validationError === 'email'} type="email" autoComplete="email" disabled={current.submitting || mutation.isPending} />{current.validationError === 'email' ? <FieldError>{t('problems:fields.invalidEmail')}</FieldError> : null}</Field>
        <fieldset className="flex flex-col gap-2" aria-describedby={current.validationError === 'roles' || current.validationError === 'invalidRoles' ? 'invite-roles-error' : undefined}>
          <legend className="text-sm font-medium">{t('role')}</legend>
          {roles.map((role) => {
            const allowed = !assignableRoleIDs || assignableRoleIDs.has(role.id)
            return (
            <label key={role.id} className="flex items-center gap-3 rounded-md border p-3 text-sm">
              <Checkbox
                checked={current.roleIDs.includes(role.id)}
                onCheckedChange={(value) => update({
                  roleIDs: value === true
                    ? [...current.roleIDs, role.id]
                    : current.roleIDs.filter((id) => id !== role.id),
                  validationError: undefined,
                })}
                aria-label={role.name}
                disabled={!allowed || current.submitting || mutation.isPending}
              />
              {role.name}
              {!allowed ? <span className="ml-auto text-xs text-muted-foreground">{t('unavailable')}</span> : null}
            </label>
            )
          })}
          {current.validationError === 'roles' || current.validationError === 'invalidRoles' ? <FieldError id="invite-roles-error">{current.validationError === 'invalidRoles' ? t('invalidRoleSelection') : t('requiredRole')}</FieldError> : null}
        </fieldset>
      </FieldGroup>
      <DialogFooter><Button type="button" variant="ghost" onClick={resetDraft}>{t('common:reset')}</Button><DialogClose asChild><Button type="button" variant="outline">{t('common:cancel')}</Button></DialogClose><Button type="submit" disabled={current.submitting || mutation.isPending}><Mail aria-hidden="true" data-icon="inline-start" />{current.submitting || mutation.isPending ? t('saving') : t('sendInvitation')}</Button></DialogFooter>
    </form>
  </>
}
