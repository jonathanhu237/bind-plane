import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { AlertDialog, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import type { ApiClient } from '@/shared/api/client'
import type { OnlineUser } from '@/shared/api/contracts'
import { notifyRequestError, notifySuccess, useRequestErrorToast } from '@/shared/feedback'
import { DataTable } from './data-table'
import { formatDate } from './access-components'
import { UserAvatar } from '@/features/auth/user-avatar'

export function OnlineUsersPage({ api, actorID, canManage }: { api: ApiClient; actorID: string; canManage: boolean }) {
  const { t, i18n } = useTranslation(['onlineUsers', 'access', 'common'])
  const client = useQueryClient()
  const [search, setSearch] = useState('')
  const [target, setTarget] = useState<OnlineUser>()
  const users = useQuery({ queryKey: ['access', 'online-users', actorID], queryFn: ({ signal }) => api.getOnlineUsers!(signal), retry: false, refetchInterval: 15_000 })
  useRequestErrorToast(users.error, users.isError, t, { title: t('loadFailed') })
  const forceSignOut = useMutation({
    mutationFn: (user: OnlineUser) => api.kickUser!(user.id),
    retry: false,
    onSuccess: (_data, user) => {
      setTarget(undefined)
      notifySuccess(t('success', { name: user.name }))
      void client.invalidateQueries({ queryKey: ['access'] })
      void client.invalidateQueries({ queryKey: ['auth', 'current-user'] })
    },
    onError: (error) => notifyRequestError(error, t, { title: t('forceSignOutFailed') }),
  })
  const columns: ColumnDef<OnlineUser, unknown>[] = [
    { accessorKey: 'name', header: t('access:inviteName'), cell: ({ row }) => <div className="flex items-center gap-2"><UserAvatar user={row.original} className="size-7 rounded-md" /><span>{row.original.name}{row.original.id === actorID ? ` (${t('you')})` : ''}</span></div> },
    { accessorKey: 'email', header: t('access:inviteEmail') },
    { accessorKey: 'sessionCount', header: t('sessions') },
    { accessorKey: 'lastSeenAt', header: t('lastSeen'), cell: ({ row }) => <time dateTime={row.original.lastSeenAt}>{formatDate(row.original.lastSeenAt, i18n.language)}</time> },
    { id: 'actions', header: t('access:actions'), cell: ({ row }) => canManage ? <Button variant="outline" size="sm" disabled={forceSignOut.isPending || users.isError} onClick={() => setTarget(row.original)}>{t('forceSignOut')}</Button> : null },
  ]
  const refreshButton = <Button variant="outline" disabled={users.isFetching} onClick={() => void users.refetch()}>{t('refresh')}</Button>
  return <section className="flex flex-col gap-5" aria-labelledby="online-title">
    <h1 id="online-title" className="text-2xl font-semibold tracking-tight">{t('title')}</h1>
    <Card>
      <CardContent className="flex flex-col gap-4 pt-6">
        {users.isPending || users.isError ? <>
          <div className="flex justify-end">{refreshButton}</div>
          <p role="status">{users.isPending ? t('common:loading') : t('loadFailed')}</p>
        </> : <DataTable columns={columns} data={users.data?.users ?? []} search={search} onSearchChange={setSearch} searchPlaceholder={t('access:searchUsers')} clearSearchLabel={t('access:clearSearch')} emptyMessage={search ? t('access:noSearchResults') : t('empty')} toolbar={refreshButton} />}
      </CardContent>
    </Card>
    <AlertDialog open={Boolean(target) && canManage} onOpenChange={(open) => { if (!open && !forceSignOut.isPending) setTarget(undefined) }}>
      <AlertDialogContent>
        <AlertDialogHeader><AlertDialogTitle>{t('confirmTitle')}</AlertDialogTitle><AlertDialogDescription>{t(target?.id === actorID ? 'confirmSelf' : 'confirm', { name: target?.name, email: target?.email })}</AlertDialogDescription></AlertDialogHeader>
        <AlertDialogFooter><AlertDialogCancel disabled={forceSignOut.isPending}>{t('common:cancel')}</AlertDialogCancel><Button variant="destructive" disabled={forceSignOut.isPending || users.isError} onClick={() => { if (target) forceSignOut.mutate(target) }}>{forceSignOut.isPending ? t('working') : t('forceSignOut')}</Button></AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  </section>
}
