import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef, OnChangeFn, SortingState } from '@tanstack/react-table'
import { RefreshCw, UserPlus, X } from 'lucide-react'
import { useMemo, useState } from 'react'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent } from '@/components/ui/dialog'
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import type { ApiClient } from '@/shared/api/client'
import type { Invitation } from '@/shared/api/contracts'
import { isForbidden, translateRateLimitedProblemWithFields } from '@/shared/api/problems'
import { DataTable, SortableHeader } from './data-table'
import { InvitationForm } from './users-page'
import { PageNavigation, RoleBadges, canAssignRole, formatDate } from './access-components'
import { invitationsOptions, roleOptionsOptions } from './queries'
import { notifyRequestError, notifySuccess, readFailureFeedback, useRequestErrorToast } from '@/shared/feedback'

type InvitationAction = 'resend' | 'renew' | 'revoke'
type InvitationSort = 'name' | 'email' | 'createdAt' | 'expiresAt'

export function InvitationsPage({ api, canManage, actorPermissions, actorSuperAdmin = false }: { api: ApiClient; canManage: boolean; actorPermissions?: string[]; actorSuperAdmin?: boolean }) {
  const { t, i18n } = useTranslation(['access', 'common', 'problems'])
  const queryClient = useQueryClient()
  const [cursor, setCursor] = useState('')
  const [history, setHistory] = useState<string[]>([])
  const [search, setSearch] = useState('')
  const [roleFilter, setRoleFilter] = useState('')
  const [statusFilter, setStatusFilter] = useState<'pending' | 'expired' | ''>('')
  const [sort, setSort] = useState<InvitationSort>('createdAt')
  const [direction, setDirection] = useState<'asc' | 'desc'>('desc')
  const [inviteOpen, setInviteOpen] = useState(false)
  const [action, setAction] = useState<{ invitation: Invitation; kind: InvitationAction }>()
  const invitations = useQuery(invitationsOptions(api, { cursor, q: search, roleId: roleFilter, status: statusFilter || undefined, sort, direction }))
  const canReadRoles = Boolean(actorSuperAdmin || actorPermissions?.includes('roles.read'))
  const roleOptions = useQuery({ ...roleOptionsOptions(api), enabled: Boolean(api.getRoleOptions) && canReadRoles })
  const roleAdministration = useQuery({
    queryKey: ['access', 'roles'],
    queryFn: ({ signal }) => api.getRoles ? api.getRoles(signal) : Promise.reject(new Error('missing getRoles')),
    retry: false,
    enabled: canManage,
    staleTime: 10_000,
  })
  const invitationsForbidden = invitations.isError && isForbidden(invitations.error)
  const roleOptionsForbidden = roleOptions.isError && isForbidden(roleOptions.error)
  const roleAdministrationForbidden = canManage && roleAdministration.isError && isForbidden(roleAdministration.error)
  useRequestErrorToast(invitations.error, invitations.isError, t, readFailureFeedback(invitations.error, { unavailableTitle: t('unavailableTitle'), unavailableDescription: t('unavailableDescription'), forbiddenTitle: t('forbiddenTitle'), forbiddenDescription: t('forbiddenDescription') }))
  useRequestErrorToast(roleOptions.error, roleOptions.isError && canReadRoles, t, readFailureFeedback(roleOptions.error, { unavailableTitle: t('unavailableTitle'), unavailableDescription: t('unavailableDescription'), forbiddenTitle: t('forbiddenTitle'), forbiddenDescription: t('forbiddenDescription') }))
  useRequestErrorToast(roleAdministration.error, roleAdministration.isError && canManage, t, readFailureFeedback(roleAdministration.error, { unavailableTitle: t('unavailableTitle'), unavailableDescription: t('unavailableDescription'), forbiddenTitle: t('forbiddenTitle'), forbiddenDescription: t('forbiddenDescription') }))
  const resend = useMutation({
    retry: false,
    mutationFn: async ({ invitation, kind }: { invitation: Invitation; kind: 'resend' | 'renew' }) => {
      if (!api.resendInvitation) throw new Error('missing resendInvitation')
      return { result: await api.resendInvitation(invitation.id), kind }
    },
    onSuccess: (_result, variables) => { setAction(undefined); notifySuccess(t(variables.kind === 'renew' ? 'invitationRenewed' : 'invitationResent')); void queryClient.invalidateQueries({ queryKey: ['access', 'invitations'] }) },
    onError: (error, variables) => notifyRequestError(error, t, { title: t(variables?.kind === 'renew' ? 'renewAndSend' : 'resend'), description: translateRateLimitedProblemWithFields(error, t, 'invitationSendRateLimited') }),
  })
  const revoke = useMutation({
    retry: false,
    mutationFn: async (invitation: Invitation) => {
      if (!api.revokeInvitation) throw new Error('missing revokeInvitation')
      await api.revokeInvitation(invitation.id)
    },
    onSuccess: () => { setAction(undefined); notifySuccess(t('invitationRevoked')); void queryClient.invalidateQueries({ queryKey: ['access', 'invitations'] }) },
    onError: (error) => notifyRequestError(error, t, { title: t('revoke') }),
  })

  const resetPaging = (value: string) => { setSearch(value); setCursor(''); setHistory([]) }
  const setFilter = (setter: (value: string) => void, value: string) => { setter(value === 'all' ? '' : value); setCursor(''); setHistory([]) }
  const handleSorting: OnChangeFn<SortingState> = (updater) => {
    const next = typeof updater === 'function' ? updater([{ id: sort, desc: direction === 'desc' }]) : updater
    const first = next[0]
    setSort((first?.id as InvitationSort | undefined) ?? 'createdAt')
    setDirection(first?.desc ? 'desc' : 'asc')
    setCursor(''); setHistory([])
  }
  const roleList = useMemo(() => roleAdministrationForbidden ? [] : roleAdministration.data?.roles ?? [], [roleAdministration.data?.roles, roleAdministrationForbidden])
  const roleFilterOptions = roleOptionsForbidden ? [] : roleOptions.data?.roles ?? []
  const assignableRoleIDs = useMemo(() => new Set(roleList.filter((role) => canAssignRole(role, actorPermissions, actorSuperAdmin)).map((role) => role.id)), [actorPermissions, actorSuperAdmin, roleList])
  const expired = (invitation: Invitation) => new Date(invitation.expiresAt).getTime() <= Date.now()

  const columns = useMemo<ColumnDef<Invitation, unknown>[]>(() => [
    {
      accessorKey: 'name',
      header: ({ column }) => <SortableHeader column={column}>{t('inviteName')}</SortableHeader>,
      cell: ({ row }) => <span className="max-w-56 truncate font-medium">{row.original.name}</span>,
    },
    {
      accessorKey: 'email',
      header: ({ column }) => <SortableHeader column={column}>{t('inviteEmail')}</SortableHeader>,
      cell: ({ row }) => <span className="text-muted-foreground">{row.original.email}</span>,
    },
    {
      id: 'roles',
      accessorFn: (invitation) => invitation.roles.map((role) => role.name).join(', '),
      header: () => <span>{t('role')}</span>,
      cell: ({ row }) => <RoleBadges roles={row.original.roles} />,
      enableSorting: false,
    },
    {
      id: 'expiresAt',
      accessorFn: (invitation) => invitation.expiresAt,
      header: ({ column }) => <SortableHeader column={column}>{t('status')}</SortableHeader>,
      cell: ({ row }) => {
        const isExpired = expired(row.original)
        return <div className="flex min-w-28 flex-col items-start gap-1"><Badge variant={isExpired ? 'destructive' : 'outline'}>{isExpired ? t('expired') : t('pending')}</Badge><time className="text-xs text-muted-foreground" dateTime={row.original.expiresAt}>{t('expiresOn', { date: formatDate(row.original.expiresAt, i18n.language) })}</time></div>
      },
    },
    {
      id: 'creator', header: t('creator'), enableSorting: false,
      cell: ({ row }) => <div className="flex flex-col gap-1"><span>{row.original.creator?.name || row.original.creator?.id || '—'}{row.original.creator?.deleted ? ` · ${t('deletedUser')}` : ''}</span><span className="text-muted-foreground">{row.original.creator?.email}</span></div>,
    },
    {
      id: 'actions',
      enableSorting: false,
      header: () => <span>{t('actions')}</span>,
      cell: ({ row }) => {
        const isExpired = expired(row.original)
        if (!canManage) return null
        const allowed = row.original.roles.every((role) => canAssignRole(role, actorPermissions, actorSuperAdmin))
        const busy = resend.isPending || revoke.isPending
        const unavailableReason = t('invitationActionUnavailable')
        const actionButton = (kind: InvitationAction, label: string, icon: React.ReactNode, variant: 'outline' | 'ghost') => {
          const button = <Button type="button" variant={variant} size="sm" disabled={!allowed || busy} aria-label={!allowed ? `${label}: ${unavailableReason}` : label} onClick={() => { setAction({ invitation: row.original, kind }) }}>{icon}{label}</Button>
          if (allowed) return button
          return <TooltipProvider><Tooltip><TooltipTrigger asChild><span tabIndex={0} className="inline-flex" aria-label={`${label}: ${unavailableReason}`}>{button}</span></TooltipTrigger><TooltipContent>{unavailableReason}</TooltipContent></Tooltip></TooltipProvider>
        }
        return <div className="flex flex-wrap justify-end gap-1">{actionButton(isExpired ? 'renew' : 'resend', isExpired ? t('renewAndSend') : t('resend'), <RefreshCw aria-hidden="true" data-icon="inline-start" />, 'outline')}{actionButton('revoke', t('revoke'), <X aria-hidden="true" data-icon="inline-start" />, 'ghost')}</div>
      },
    },
  ], [actorPermissions, actorSuperAdmin, canManage, i18n.language, resend.isPending, revoke.isPending, t])

  if (invitations.isPending || (canManage && roleAdministration.isPending)) return <p role="status">{t('common:loading')}</p>
  return <section className="flex flex-col gap-5" aria-labelledby="invitations-title">
    <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between"><h1 id="invitations-title" className="text-2xl font-semibold tracking-tight">{t('invitationsTitle')}</h1>{canManage && !roleAdministrationForbidden ? <Button type="button" onClick={() => setInviteOpen(true)}><UserPlus aria-hidden="true" data-icon="inline-start" />{t('inviteUser')}</Button> : null}</div>
    <Card>
      <CardContent className="pt-6">
        {invitations.isError && (invitationsForbidden || !invitations.data) ? <p role="status" className="text-sm text-muted-foreground">{invitationsForbidden ? t('forbiddenDescription') : t('common:refreshPage')}</p> : <>
        <DataTable
          columns={columns}
          data={invitations.data?.invitations ?? []}
          search={search}
          onSearchChange={resetPaging}
          searchPlaceholder={t('searchInvitations')}
          clearSearchLabel={t('clearSearch')}
          emptyMessage={search || roleFilter || statusFilter ? t('noSearchResults') : t('noInvitations')}
          sorting={[{ id: sort, desc: direction === 'desc' }]}
          onSortingChange={handleSorting}
          manualFiltering
          manualSorting
          toolbar={<div className="flex flex-wrap items-center gap-2">{canReadRoles && !roleOptionsForbidden ? <Select value={roleFilter || 'all'} onValueChange={(value) => setFilter(setRoleFilter, value)}><SelectTrigger className="w-44" aria-label={t('filterByRole')}><SelectValue placeholder={t('allRoles')} /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="all">{t('allRoles')}</SelectItem>{roleFilterOptions.map((role) => <SelectItem key={role.id} value={role.id}>{role.name}</SelectItem>)}</SelectGroup></SelectContent></Select> : null}<Select value={statusFilter || 'all'} onValueChange={(value) => { setStatusFilter(value === 'all' ? '' : value as 'pending' | 'expired'); setCursor(''); setHistory([]) }}><SelectTrigger className="w-36" aria-label={t('filterByStatus')}><SelectValue placeholder={t('allStatuses')} /></SelectTrigger><SelectContent><SelectGroup><SelectItem value="all">{t('allStatuses')}</SelectItem><SelectItem value="pending">{t('pending')}</SelectItem><SelectItem value="expired">{t('expired')}</SelectItem></SelectGroup></SelectContent></Select></div>}
        />
        {invitations.isFetching && !invitations.isPending ? <p role="status" className="mt-3 text-sm text-muted-foreground">{t('common:loading')}</p> : null}
        <PageNavigation hasPrevious={history.length > 0} hasNext={Boolean(invitations.data?.nextCursor)} loading={invitations.isFetching} onPrevious={() => { const previous = history[history.length - 1] ?? ''; setHistory((current) => current.slice(0, -1)); setCursor(previous) }} onNext={() => { if (!invitations.data?.nextCursor) return; setHistory((current) => [...current, cursor]); setCursor(invitations.data.nextCursor) }} t={(key) => t(key as never)} />
        </>}
      </CardContent>
    </Card>
    <Dialog open={inviteOpen && !roleAdministrationForbidden} onOpenChange={setInviteOpen}>
      <DialogContent forceMount closeLabel={t('common:close')} className="max-h-[90dvh] overflow-y-auto sm:max-w-lg"><InvitationForm open={inviteOpen} api={api} roles={roleList} assignableRoleIDs={assignableRoleIDs} onDone={() => { setInviteOpen(false); void queryClient.invalidateQueries({ queryKey: ['access', 'invitations'] }) }} /></DialogContent>
    </Dialog>
    <AlertDialog open={Boolean(action) && !invitationsForbidden} onOpenChange={(open) => { if (!open) setAction(undefined) }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{action?.kind === 'revoke' ? t('revoke') : action?.kind === 'renew' ? t('renewInvitation') : t('resend')}</AlertDialogTitle>
          <AlertDialogDescription>{action?.kind === 'revoke' ? t('revokeConfirm', { email: action.invitation.email }) : t('sendInvitationConfirm', { email: action?.invitation.email ?? '' })}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t('common:cancel')}</AlertDialogCancel>
          <AlertDialogAction disabled={resend.isPending || revoke.isPending} onClick={() => { if (!action) return; if (action.kind === 'revoke') revoke.mutate(action.invitation); else resend.mutate({ invitation: action.invitation, kind: action.kind }) }}>{action?.kind === 'revoke' ? t('revoke') : action?.kind === 'renew' ? t('renewAndSend') : t('resend')}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </section>
}
