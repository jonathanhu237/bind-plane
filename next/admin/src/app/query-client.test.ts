import { describe, expect, it } from 'vitest'
import { ApiProblemError } from '@/shared/api/client'
import { clearAccessDrafts, useAccessDraftStore } from '@/features/access/drafts'
import { createAppQueryClient } from './query-client'

describe('application query client', () => {
  it('clears access drafts and caches when a session expires', async () => {
    const queryClient = createAppQueryClient()
    useAccessDraftStore.getState().setOwner('019535d9-3df7-79fb-b466-fa907fa17f91')
    useAccessDraftStore.getState().setRoleCreate({ name: 'Auditor', description: '', permissions: [], submitting: false, conflict: false })
    queryClient.setQueryData(['access', 'users'], { users: ['stale'] })
    queryClient.setQueryData(['settings', 'email'], { host: 'stale.example.test', password: 'stale-secret' })
    queryClient.setQueryData(['settings', 'email', 'test', 'stale-task'], { status: 'sent' })

    const error = new ApiProblemError({ type: '/problems/unauthenticated', title: 'expired', status: 401, code: 'unauthenticated' })
    await expect(queryClient.fetchQuery({ queryKey: ['access', 'expired'], queryFn: async () => { throw error } })).rejects.toBe(error)

    expect(queryClient.getQueryData(['access', 'users'])).toBeUndefined()
    expect(queryClient.getQueryData(['settings', 'email'])).toBeUndefined()
    expect(queryClient.getQueryData(['settings', 'email', 'test', 'stale-task'])).toBeUndefined()
    expect(useAccessDraftStore.getState().ownerID).toBeUndefined()
    expect(useAccessDraftStore.getState().roleCreate).toBeUndefined()
    clearAccessDrafts()
  })
})
