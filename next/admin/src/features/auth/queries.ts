import { queryOptions } from '@tanstack/react-query'
import type { ApiClient } from '@/shared/api/client'

export const setupStatusQueryKey = ['setup', 'status'] as const
export const currentUserQueryKey = ['auth', 'current-user'] as const

export function setupStatusOptions(api: ApiClient) {
  return queryOptions({
    queryKey: setupStatusQueryKey,
    queryFn: ({ signal }) => api.getSetupStatus(signal),
    retry: false,
    staleTime: 0,
  })
}

export function currentUserOptions(api: ApiClient) {
  return queryOptions({
    queryKey: currentUserQueryKey,
    queryFn: ({ signal }) => api.me(signal),
    retry: false,
    staleTime: 0,
  })
}
