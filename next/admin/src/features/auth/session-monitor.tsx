import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { ApiClient } from '@/shared/api/client'
import { isAccountDisabled, isUnauthenticated } from '@/shared/api/problems'
import { clearAccessDrafts } from '@/features/access/drafts'
import { restoreGuestLocale } from '@/shared/i18n'

// Keep the heartbeat separate from route-loader data: an expired session must
// redirect even while the user stays on the same page.
export function SessionMonitor({ api, userID }: { api: ApiClient; userID: string }) {
  const { t } = useTranslation(['auth', 'problems'])
  const client = useQueryClient()
  const navigate = useNavigate()
  const session = useQuery({ queryKey: ['session-monitor', userID], queryFn: ({ signal }) => api.checkSession ? api.checkSession(signal) : Promise.reject(new Error('Session status probe is unavailable')), retry: false, refetchInterval: 30_000, refetchIntervalInBackground: true, refetchOnWindowFocus: true, staleTime: 0 })
  useEffect(() => {
    if (!isUnauthenticated(session.error)) return
    toast.error(isAccountDisabled(session.error) ? t('problems:accountDisabled') : t('sessionExpired'))
    clearAccessDrafts()
    client.clear()
    void restoreGuestLocale()
    void navigate({ to: '/login', replace: true })
  }, [session.error, client, navigate, t])
  return null
}
