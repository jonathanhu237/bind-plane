import { QueryClient } from '@tanstack/react-query'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { createApiClient } from '@/shared/api/client'
import { server } from '@/test/msw'
import { currentUserOptions, currentUserQueryKey, setupStatusOptions, setupStatusQueryKey } from './queries'

describe('auth query ownership', () => {
  it('shares one status cache and removes the user only after logout succeeds', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const api = createApiClient()
    server.use(
      http.get('/api/setup/status', () => HttpResponse.json({ status: 'complete' })),
      http.get('/api/auth/me', () => HttpResponse.json({
        user: { id: '00000000-0000-4000-8000-000000000001', name: 'Admin', email: 'admin@example.com' },
      })),
    )

    await expect(queryClient.fetchQuery(setupStatusOptions(api))).resolves.toEqual({ status: 'complete' })
    expect(queryClient.getQueryData(setupStatusQueryKey)).toEqual({ status: 'complete' })
    const currentUser = await queryClient.fetchQuery(currentUserOptions(api))
    expect(queryClient.getQueryData(currentUserQueryKey)).toEqual(currentUser)
    queryClient.removeQueries({ queryKey: currentUserQueryKey })
    expect(queryClient.getQueryData(currentUserQueryKey)).toBeUndefined()
  })
})
