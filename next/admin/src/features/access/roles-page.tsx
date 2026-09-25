import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef, OnChangeFn, SortingState } from '@tanstack/react-table'
import { Eye, LockKeyhole, MoreHorizontal, Pencil, Plus, Save, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogClose, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import type { Permission, PermissionCombination, Role } from '@/shared/api/contracts'
import { isForbidden } from '@/shared/api/problems'
import { AssignmentCount, AssignmentCountInfo, BuiltInRoleIndicator } from './access-components'
import { DataTable, SortableHeader } from './data-table'
import { nextDraftSubmissionID, useAccessDraftStore } from './drafts'
import { roleQueryKey, rolesOptions, rolesQueryKey } from './queries'
import { notifyRequestError, notifySuccess, readFailureFeedback, useRequestErrorToast } from '@/shared/feedback'
import { systemIdentityName, usePublicSystemIdentity } from '@/features/identity/system-identity'

export function RolesPage({ api, canManage, actorPermissions, actorSuperAdmin = false }: { api: ApiClient; canManage: boolean; actorPermissions?: string[]; actorSuperAdmin?: boolean }) {
  const { t } = useTranslation(['access', 'problems', 'common'])
  const queryClient = useQueryClient()
  const query = useQuery(rolesOptions(api))
  const identity = usePublicSystemIdentity(api)
  const systemName = systemIdentityName(identity.data)
  const [selected, setSelected] = useState<Role | undefined>()
  const [detailOpen, setDetailOpen] = useState(false)
  const [editorOpen, setEditorOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Role | undefined>()
  const [search, setSearch] = useState('')
  const [sorting, setSorting] = useState<SortingState>([])
  const [editorGeneration, setEditorGeneration] = useState(0)
  const editorGenerationRef = useRef(0)
  const advanceEditorGeneration = () => {
    const next = editorGenerationRef.current + 1
    editorGenerationRef.current = next
    setEditorGeneration(next)
  }
  const roles = useMemo(() => query.data?.roles ?? [], [query.data?.roles])
  const rolesForbidden = query.isError && isForbidden(query.error)
  useRequestErrorToast(query.error, query.isError, t, readFailureFeedback(query.error, { unavailableTitle: t('unavailableTitle'), unavailableDescription: t('unavailableDescription'), forbiddenTitle: t('forbiddenTitle'), forbiddenDescription: t('forbiddenDescription') }))
  const selectedRole = selected ? roles.find((role) => role.id === selected.id) ?? selected : undefined
  const filteredRoles = useMemo(() => {
    const needle = search.trim().toLocaleLowerCase()
    return needle ? roles.filter((role) => role.name.toLocaleLowerCase().includes(needle)) : roles
  }, [roles, search])

  const deleteMutation = useMutation({
    retry: false,
    mutationFn: async (role: Role) => {
      if (!api.deleteRole) throw new Error('missing deleteRole')
      await api.deleteRole(role.id)
    },
    onSuccess: (_, role) => {
      setSelected((current) => current?.id === role.id ? undefined : current)
      setDeleteTarget(undefined)
      useAccessDraftStore.getState().setRoleEdit(role.id, undefined)
      notifySuccess(t('roleDeleted'))
      void queryClient.invalidateQueries({ queryKey: rolesQueryKey })
    },
    onError: (error) => notifyRequestError(error, t, { title: t('deleteRole') }),
  })

  const openCreate = () => {
    advanceEditorGeneration()
    setSelected(undefined)
    setEditorOpen(true)
    setDetailOpen(false)
  }
  const openDetail = useCallback((role: Role) => {
    setSelected(role)
    setDetailOpen(true)
    setEditorOpen(false)
  }, [])
  const openEdit = useCallback((role: Role) => {
    if (!canManage || role.system) return
    advanceEditorGeneration()
    setSelected(role)
    setDetailOpen(false)
    setEditorOpen(true)
  }, [canManage])
  const handleSorting: OnChangeFn<SortingState> = (updater) => {
    setSorting((current) => typeof updater === 'function' ? updater(current) : updater)
  }
  const columns = useMemo<ColumnDef<Role, unknown>[]>(() => [
    {
      accessorKey: 'name',
      header: ({ column }) => <SortableHeader column={column}>{t('roleName')}</SortableHeader>,
      cell: ({ row }) => <div className="flex min-w-0 items-center gap-2"><button type="button" className="min-w-0 truncate text-left font-medium" onClick={() => openDetail(row.original)}>{row.original.name}</button>{row.original.system ? <BuiltInRoleIndicator label={t('builtInRoleTooltip', { systemName })} /> : null}</div>,
    },
    {
      accessorKey: 'description',
      enableSorting: false,
      header: () => <span>{t('roleDescription')}</span>,
      cell: ({ row }) => <span className="max-w-64 truncate text-sm text-muted-foreground">{row.original.description || '—'}</span>,
    },
    {
      id: 'permissions',
      accessorFn: (role) => role.permissions.length,
      header: ({ column }) => <SortableHeader column={column}>{t('permissionCount')}</SortableHeader>,
      cell: ({ row }) => <span>{row.original.permissions.length}</span>,
    },
    {
      id: 'assignments',
      accessorFn: (role) => role.assignmentCount ?? 0,
      header: ({ column }) => <div className="flex items-center gap-1"><SortableHeader column={column}>{t('assignmentCount')}</SortableHeader><AssignmentCountInfo /></div>,
      cell: ({ row }) => <AssignmentCount count={row.original.assignmentCount ?? 0} />,
    },
    {
      id: 'actions',
      enableSorting: false,
      header: () => <span>{t('actions')}</span>,
      cell: ({ row }) => {
        const role = row.original
        return <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild><Button type="button" variant="ghost" size="icon" aria-label={t('moreActions')}><MoreHorizontal aria-hidden="true" /></Button></DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => openDetail(role)}><Eye aria-hidden="true" data-icon="inline-start" />{t('view')}</DropdownMenuItem>
            {canManage && !role.system ? <DropdownMenuItem onSelect={() => openEdit(role)}><Pencil aria-hidden="true" data-icon="inline-start" />{t('edit')}</DropdownMenuItem> : null}
            {canManage && !role.system ? <><DropdownMenuSeparator />{(() => {
              const disabled = (role.assignmentCount ?? 0) > 0
              const item = <DropdownMenuItem disabled={disabled} title={disabled ? t('deleteRoleDisabled') : undefined} onSelect={() => { if (!disabled) setDeleteTarget(role) }}><Trash2 aria-hidden="true" data-icon="inline-start" />{t('delete')}</DropdownMenuItem>
              return disabled ? <TooltipProvider><Tooltip><TooltipTrigger asChild><span tabIndex={0} className="block" aria-label={`${t('delete')}: ${t('deleteRoleDisabled')}`}>{item}</span></TooltipTrigger><TooltipContent>{t('deleteRoleDisabled')}</TooltipContent></Tooltip></TooltipProvider> : item
            })()}</> : null}
          </DropdownMenuContent>
        </DropdownMenu>
      },
    },
  ], [canManage, openDetail, openEdit, systemName, t])

  if (query.isPending) return <p role="status">{t('common:loading')}</p>
  return <section className="flex flex-col gap-5" aria-labelledby="roles-title">
    <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between"><h1 id="roles-title" className="text-2xl font-semibold tracking-tight">{t('rolesTitle')}</h1>{canManage && !rolesForbidden ? <Button type="button" onClick={openCreate}><Plus aria-hidden="true" data-icon="inline-start" />{t('createRole')}</Button> : null}</div>
    <Card>
      <CardContent className="pt-6">{query.isError && (rolesForbidden || !query.data) ? <p role="status" className="text-sm text-muted-foreground">{rolesForbidden ? t('forbiddenDescription') : t('common:refreshPage')}</p> : <DataTable columns={columns} data={filteredRoles} search={search} onSearchChange={setSearch} searchPlaceholder={t('searchRoles')} clearSearchLabel={t('clearSearch')} emptyMessage={search ? t('noSearchResults') : t('noRoles')} manualFiltering sorting={sorting} onSortingChange={handleSorting} />}</CardContent>
    </Card>
    <Dialog open={detailOpen && !rolesForbidden} onOpenChange={setDetailOpen}>
      <DialogContent forceMount closeLabel={t('common:close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        {selectedRole ? <RoleDetail role={selectedRole} permissions={query.data?.permissions ?? []} canManage={canManage} onEdit={() => openEdit(selectedRole)} /> : null}
      </DialogContent>
    </Dialog>
    <Dialog open={editorOpen && !rolesForbidden} onOpenChange={setEditorOpen}>
      <DialogContent forceMount closeLabel={t('common:close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-2xl">
        <RoleEditor key={selectedRole?.id ?? 'new'} api={api} role={selectedRole} permissions={query.data?.permissions ?? []} combinations={query.data?.combinations ?? []} actorPermissions={actorPermissions} actorSuperAdmin={actorSuperAdmin || actorPermissions === undefined} open={editorOpen} editorGeneration={editorGeneration} onDone={(role, generation) => { if (generation !== editorGenerationRef.current) return; setSelected(role); setEditorOpen(false); void queryClient.invalidateQueries({ queryKey: rolesQueryKey }); void queryClient.invalidateQueries({ queryKey: roleQueryKey(role.id) }) }} />
      </DialogContent>
    </Dialog>
    <AlertDialog open={Boolean(deleteTarget) && !rolesForbidden} onOpenChange={(open) => { if (!open) setDeleteTarget(undefined) }}>
      <AlertDialogContent><AlertDialogHeader><AlertDialogTitle>{t('deleteRole')}</AlertDialogTitle><AlertDialogDescription>{t('deleteRoleConfirm', { name: deleteTarget?.name ?? '' })}</AlertDialogDescription></AlertDialogHeader><AlertDialogFooter><AlertDialogCancel>{t('common:cancel')}</AlertDialogCancel><AlertDialogAction disabled={deleteMutation.isPending || (deleteTarget?.assignmentCount ?? 0) > 0} onClick={() => { if (deleteTarget && (deleteTarget.assignmentCount ?? 0) === 0) deleteMutation.mutate(deleteTarget) }}>{t('delete')}</AlertDialogAction></AlertDialogFooter></AlertDialogContent>
    </AlertDialog>
  </section>
}

function RoleDetail({ role, permissions, canManage, onEdit }: { role: Role; permissions: Permission[]; canManage: boolean; onEdit: () => void }) {
  const { t } = useTranslation(['access', 'common'])
  const translate = (key: string) => t(key as never)
  const definitions = new Map(permissions.map((permission) => [permission.key, permission]))
  const groups = Object.entries(role.permissions.reduce<Record<string, Permission[]>>((result, key) => {
    const permission = definitions.get(key)
    if (!permission) return result
    const group = result[permission.resource] ?? []
    group.push(permission)
    result[permission.resource] = group
    return result
  }, {})).sort(([left], [right]) => left.localeCompare(right))
  return <>
    <DialogHeader><DialogTitle>{role.name}</DialogTitle></DialogHeader>
    <div className="flex flex-col gap-5"><p className="text-sm text-muted-foreground">{role.description || '—'}</p><div><h3 className="mb-2 text-sm font-medium">{t('permissions')} ({role.permissions.length})</h3>{groups.length > 0 ? <div className="flex flex-col gap-4">{groups.map(([resource, items]) => <section key={resource} className="rounded-md bg-muted/30 p-3"><h4 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{localizedResource(resource, translate)}</h4><ul className="mt-2 flex flex-col gap-2">{items.map((permission) => <li key={permission.key} className="flex flex-col"><span className="text-sm font-medium">{localizedPermission(permission.key, translate)}</span><span className="text-xs text-muted-foreground">{localizedPermissionDescription(permission.key, translate, permission.description)}</span></li>)}</ul></section>)}</div> : <p className="text-sm text-muted-foreground">{t('noPermissions')}</p>}</div><p className="text-sm text-muted-foreground">{t('assignmentCountDetail', { count: role.assignmentCount ?? 0 })}</p></div>
    <DialogFooter><DialogClose asChild><Button type="button" variant="outline">{t('common:close')}</Button></DialogClose>{canManage && !role.system ? <Button type="button" onClick={onEdit}><Pencil aria-hidden="true" data-icon="inline-start" />{t('edit')}</Button> : null}</DialogFooter>
  </>
}

function RoleEditor({ api, role, permissions, combinations, actorPermissions, actorSuperAdmin, open, editorGeneration, onDone }: { api: ApiClient; role?: Role; permissions: Permission[]; combinations: PermissionCombination[]; actorPermissions?: string[]; actorSuperAdmin: boolean; open: boolean; editorGeneration: number; onDone: (role: Role, generation: number) => void }) {
  const { t } = useTranslation(['access', 'problems', 'common'])
  const queryClient = useQueryClient()
  const roleCreate = useAccessDraftStore((state) => state.roleCreate)
  const roleEdit = useAccessDraftStore((state) => role ? state.roleEdits[role.id] : undefined)
  const setRoleCreate = useAccessDraftStore((state) => state.setRoleCreate)
  const setRoleEdit = useAccessDraftStore((state) => state.setRoleEdit)
  const conflictAnnounced = useRef<string | undefined>(undefined)
  const draft = role ? roleEdit : roleCreate
  const initial = useMemo(() => role
    ? { id: role.id, revision: role.revision, name: role.name, description: role.description, permissions: role.permissions.slice(), explicitPermissions: role.permissions.slice() }
    : { name: '', description: '', permissions: [] as string[], explicitPermissions: [] as string[] }, [role])

  useEffect(() => {
    if (!open) return
    if (!role) {
      if (!roleCreate) setRoleCreate({ ...initial, submitting: false, conflict: false })
      return
    }
    if (!roleEdit) {
      setRoleEdit(role.id, { ...initial, submitting: false, conflict: false })
      return
    }
    if (roleEdit.revision !== role.revision && !roleEdit.conflict) {
      setRoleEdit(role.id, { ...roleEdit, conflict: true })
      const signature = `${role.id}:${roleEdit.revision}:${role.revision}`
      if (conflictAnnounced.current !== signature) {
        conflictAnnounced.current = signature
        notifyRequestError(new ApiProblemError({ type: '/problems/stale-revision', title: 'stale revision', status: 409, code: 'stale_revision' }), t, { title: t('conflictTitle'), description: t('draftConflictDescription') })
      }
    }
  }, [initial, open, role, roleCreate, roleEdit, setRoleCreate, setRoleEdit, t])

  const current = draft ?? { ...initial, submitting: false, conflict: false }
  type Submission = { ownerID?: string; submissionID: string }
  const updateDraft = (patch: Partial<typeof current>, submission?: Submission): boolean => {
    const state = useAccessDraftStore.getState()
    const existing = role ? state.roleEdits[role.id] : state.roleCreate
    if (submission && (state.ownerID !== submission.ownerID || existing?.submissionID !== submission.submissionID)) return false
    const next = { ...(existing ?? current), ...patch }
    if (role) setRoleEdit(role.id, next)
    else setRoleCreate(next)
    return true
  }
  const update = (patch: Partial<typeof current>) => {
    updateDraft(patch)
  }
  const beginSubmission = (): Submission => {
    const state = useAccessDraftStore.getState()
    const existing = role ? state.roleEdits[role.id] : state.roleCreate
    const submission = { ownerID: state.ownerID, submissionID: nextDraftSubmissionID() }
    updateDraft({ ...(existing ?? current), submissionID: submission.submissionID, submitting: false, conflict: false })
    return submission
  }
  const resetDraft = () => {
    const next = { ...initial, submitting: false, conflict: false, validationError: undefined }
    if (role) setRoleEdit(role.id, next)
    else setRoleCreate(next)
  }
	const permissionGroups = Object.entries(permissions.reduce<Record<string, Permission[]>>((groups, permission) => {
    const group = groups[permission.resource] ?? []
    group.push(permission)
    groups[permission.resource] = group
    return groups
	}, {})).sort(([left], [right]) => left.localeCompare(right))
	const translate = (key: string) => t(key as never)
	const actorGrantable = new Set(actorPermissions ?? [])
  const mutation = useMutation({
    retry: false,
    mutationFn: async (submission: Submission) => {
      const state = useAccessDraftStore.getState()
      const draft = role ? state.roleEdits[role.id] : state.roleCreate
      if (!draft || state.ownerID !== submission.ownerID || draft.submissionID !== submission.submissionID) throw new Error('stale draft')
      if (!draft.name.trim()) { updateDraft({ validationError: 'name' }, submission); throw new Error('invalid name') }
      if (draft.permissions.length === 0) { updateDraft({ validationError: 'permissions' }, submission); throw new Error('invalid permissions') }
      const availablePermissions = new Set(permissions.map((permission) => permission.key))
      if (draft.permissions.some((permission) => !availablePermissions.has(permission))) {
        updateDraft({ validationError: 'invalidPermissions' }, submission)
        throw new Error('invalid permissions')
      }
      if (!updateDraft({ submitting: true, validationError: undefined, conflict: false }, submission)) throw new Error('stale draft')
      if (role) {
        if (!api.replaceRole) throw new Error('missing replaceRole')
        return api.replaceRole(role.id, { name: draft.name, description: draft.description, permissions: draft.permissions, revision: draft.revision ?? role.revision })
      }
      if (!api.createRole) throw new Error('missing createRole')
      return api.createRole({ name: draft.name, description: draft.description, permissions: draft.permissions })
    },
    onSuccess: (saved, submission) => {
      const state = useAccessDraftStore.getState()
      if (state.ownerID !== submission.ownerID) return
      void queryClient.invalidateQueries({ queryKey: rolesQueryKey })
      if (role) void queryClient.invalidateQueries({ queryKey: roleQueryKey(role.id) })
      const existing = role ? state.roleEdits[role.id] : state.roleCreate
      if (existing?.submissionID !== submission.submissionID) return
      if (role) setRoleEdit(role.id, undefined)
      else setRoleCreate(undefined)
      notifySuccess(t(role ? 'roleUpdated' : 'roleCreated'))
      onDone(saved, editorGeneration)
    },
    onError: (error, submission) => {
      if (!submission) return
      if (!updateDraft({ submitting: false }, submission)) return
      if (isStaleRevision(error)) {
        updateDraft({ conflict: true }, submission)
        notifyRequestError(error, t, { title: t('conflictTitle'), description: t('draftConflictDescription') })
      } else if (!(error instanceof Error && (error.message === 'invalid name' || error.message === 'invalid permissions'))) {
        notifyRequestError(error, t, { title: t(role ? 'saveRole' : 'createRole') })
      }
    },
  })

  return <>
    <DialogHeader>
      <DialogTitle>{role ? t('editRole') : t('createRole')}</DialogTitle>
    </DialogHeader>
    <form className="flex flex-col gap-5" onSubmit={(event) => { event.preventDefault(); if (!current.submitting && !mutation.isPending && !current.conflict) mutation.mutate(beginSubmission()) }} noValidate>
      <FieldGroup>
        <Field data-invalid={current.validationError === 'name'}>
          <FieldLabel htmlFor="role-name">{t('roleName')}</FieldLabel>
          <Input id="role-name" value={current.name} onChange={(event) => update({ name: event.target.value, validationError: undefined })} aria-invalid={current.validationError === 'name'} disabled={current.submitting || mutation.isPending || current.conflict} />
          {current.validationError === 'name' ? <FieldError>{t('problems:fields.invalidValue')}</FieldError> : null}
        </Field>
        <Field>
          <FieldLabel htmlFor="role-description">{t('roleDescription')}</FieldLabel>
          <textarea id="role-description" className="min-h-24 w-full rounded-md border border-input bg-background px-3 py-2 text-sm focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring" value={current.description} onChange={(event) => update({ description: event.target.value })} disabled={current.submitting || mutation.isPending || current.conflict} />
        </Field>
        <fieldset className="flex flex-col gap-4" aria-describedby={current.validationError === 'permissions' || current.validationError === 'invalidPermissions' ? 'role-permissions-error' : undefined}>
          <legend className="text-sm font-medium">{t('permissions')}</legend>
			{permissionGroups.map(([resource, items]) => <fieldset key={resource} className="flex flex-col gap-2 rounded-md bg-muted/30 p-3">
            <legend className="px-1 text-xs font-semibold uppercase tracking-wide text-muted-foreground">{localizedResource(resource, translate)}</legend>
			{items.map((permission) => {
				const checked = current.permissions.includes(permission.key)
				const lockedByActor = !actorSuperAdmin && !actorGrantable.has(permission.key)
				const lockedByFeature = isRequiredBySelectedWrite(permission.key, current.permissions, combinations)
				const locked = lockedByActor || lockedByFeature
				const unavailableCombination = !actorSuperAdmin && isWritePermission(permission.key) && !combinationGrantable(permission.key, actorGrantable, combinations)
				const label = localizedPermission(permission.key, translate)
            return <label key={permission.key} className="flex items-start gap-3 rounded-md border bg-background p-3">
						<Checkbox checked={checked} onCheckedChange={(value) => {
						const explicit = current.explicitPermissions ?? current.permissions
						if (value === true) {
							update({ permissions: addFeaturePermissions(current.permissions, permission.key, combinations), explicitPermissions: Array.from(new Set([...explicit, permission.key])), validationError: undefined })
							return
						}
						const nextExplicit = explicit.filter((key) => key !== permission.key)
						// Keep permissions that were added for a feature visible after its
						// write grant is removed. The administrator can explicitly clear
						// them afterwards; silently dropping unrelated read access would
						// make a role edit destructive and hard to review.
						const nextPermissions = current.permissions.filter((key) => key !== permission.key)
						update({ permissions: nextPermissions, explicitPermissions: nextExplicit, validationError: undefined })
						}} aria-label={label} disabled={locked || unavailableCombination || current.submitting || mutation.isPending || current.conflict} />
					<span className="min-w-0 flex-1"><span className="flex items-center gap-2 text-sm font-medium">{label}{locked ? <LockKeyhole aria-label={t('permissionRequired')} /> : null}</span><span className="block text-xs text-muted-foreground">{localizedPermissionDescription(permission.key, translate, permission.description)}{locked ? ` · ${t('permissionRequired')}` : ''}</span></span>
              </label>
            })}
          </fieldset>)}
          {current.validationError === 'permissions' || current.validationError === 'invalidPermissions' ? <FieldError id="role-permissions-error">{current.validationError === 'invalidPermissions' ? t('invalidPermissionSelection') : t('requiredPermission')}</FieldError> : null}
        </fieldset>
      </FieldGroup>
      <DialogFooter>
        <Button type="button" variant="ghost" onClick={resetDraft} disabled={current.conflict}>{t('common:reset')}</Button>
        <DialogClose asChild><Button type="button" variant="outline">{t('common:cancel')}</Button></DialogClose>
        <Button type="submit" disabled={current.submitting || mutation.isPending || current.conflict}><Save aria-hidden="true" />{current.submitting || mutation.isPending ? t('saving') : t('saveRole')}</Button>
      </DialogFooter>
    </form>
  </>
}

function isStaleRevision(error: unknown): boolean {
  return error instanceof ApiProblemError && (error.problem.code === 'stale_revision' || error.problem.type === '/problems/stale-revision')
}

const permissionLabelKeys: Record<string, string> = {
 'online-users.read': 'permissionOnlineUsersRead',
 'online-users.write': 'permissionOnlineUsersWrite',
  'users.read': 'permissionUsersRead',
  'users.write': 'permissionUsersWrite',
  'roles.read': 'permissionRolesRead',
  'roles.write': 'permissionRolesWrite',
  'invitations.read': 'permissionInvitationsRead',
  'invitations.write': 'permissionInvitationsWrite',
  'settings.read': 'permissionSettingsRead',
  'settings.write': 'permissionSettingsWrite',
  'operation-logs.read': 'permissionOperationLogsRead',
  'mail-tasks.read': 'permissionMailTasksRead',
  'mail-tasks.write': 'permissionMailTasksWrite',
}

const permissionDescriptionKeys: Record<string, string> = {
 'online-users.read': 'permissionOnlineUsersReadDescription',
 'online-users.write': 'permissionOnlineUsersWriteDescription',
  'users.read': 'permissionUsersReadDescription',
  'users.write': 'permissionUsersWriteDescription',
  'roles.read': 'permissionRolesReadDescription',
  'roles.write': 'permissionRolesWriteDescription',
  'invitations.read': 'permissionInvitationsReadDescription',
  'invitations.write': 'permissionInvitationsWriteDescription',
  'settings.read': 'permissionSettingsReadDescription',
  'settings.write': 'permissionSettingsWriteDescription',
  'operation-logs.read': 'permissionOperationLogsReadDescription',
  'mail-tasks.read': 'permissionMailTasksReadDescription',
  'mail-tasks.write': 'permissionMailTasksWriteDescription',
}

function localizedPermission(key: string, t: (key: string) => string): string {
  return permissionLabelKeys[key] ? t(permissionLabelKeys[key]) : key
}

function localizedPermissionDescription(key: string, t: (key: string) => string, fallback: string): string {
  return permissionDescriptionKeys[key] ? t(permissionDescriptionKeys[key]) : fallback
}

const resourceLabelKeys: Record<string, string> = {
 'online-users': 'onlineUsers',
  users: 'users',
  roles: 'roles',
  invitations: 'invitations',
  settings: 'settings',
  'operation-logs': 'operationLog',
  'mail-tasks': 'emailTasks',
}

function localizedResource(resource: string, t: (key: string) => string): string {
	return resourceLabelKeys[resource] ? t(resourceLabelKeys[resource]) : resource
}

function addFeaturePermissions(selected: string[], key: string, combinations: PermissionCombination[]): string[] {
	const result = new Set(selected)
	result.add(key)
	for (const combination of combinations) {
		if (!combinationActive(combination, result)) continue
		for (const permission of combination.permissions) result.add(permission)
	}
	return Array.from(result)
}

function isWritePermission(key: string): boolean {
	return key.endsWith('.write')
}

function combinationGrantable(key: string, actorPermissions: Set<string>, combinations: PermissionCombination[]): boolean {
	if (!isWritePermission(key)) return true
	let found = false
	for (const combination of combinations) {
		const trigger = combinationTrigger(combination)
		if (!trigger.includes(key)) continue
		found = true
		if (combination.permissions.every((permission) => actorPermissions.has(permission))) return true
	}
	return !found
}

function isRequiredBySelectedWrite(key: string, selected: string[], combinations: PermissionCombination[]): boolean {
	if (isWritePermission(key)) return false
	const granted = new Set(selected)
	return combinations.some((combination) => combinationActive(combination, granted) && combination.permissions.includes(key) && !combinationTrigger(combination).includes(key))
}

function combinationTrigger(combination: PermissionCombination): string[] {
	// Older embedders may omit trigger metadata. Preserve their previous
	// behavior by treating the write permissions in the combination as the
	// activation set until they can upgrade their API response.
	return combination.trigger?.length ? combination.trigger : combination.permissions.filter(isWritePermission)
}

function combinationActive(combination: PermissionCombination, selected: Set<string>): boolean {
	return combinationTrigger(combination).every((permission) => selected.has(permission))
}
