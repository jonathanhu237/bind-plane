import { QueryClient } from '@tanstack/react-query'
import { describe, expect, it, vi } from 'vitest'
import { createAppRouter } from '@/app/router'
import type { ApiClient } from '@/shared/api/client'

function routeApi(): ApiClient {
  return {
    getSetupStatus: vi.fn(),
    setup: vi.fn(),
    login: vi.fn(),
    me: vi.fn(),
    logout: vi.fn(),
    requestPasswordReset: vi.fn(),
    completePasswordReset: vi.fn(),
  }
}

describe('access routes', () => {
  it('registers users, invitations, and roles under the authenticated route boundary', () => {
    const router = createAppRouter({ api: routeApi(), queryClient: new QueryClient() })
    const rolesRoute = router.routesByPath['/roles']
    const usersRoute = router.routesByPath['/users']
    const invitationsRoute = router.routesByPath['/invitations']

    expect(rolesRoute).toBeDefined()
    expect(usersRoute).toBeDefined()
    expect(invitationsRoute).toBeDefined()
    expect(rolesRoute.id).toBe('/_authenticated/roles')
    expect(usersRoute.id).toBe('/_authenticated/users')
    expect(invitationsRoute.id).toBe('/_authenticated/invitations')
    expect(rolesRoute.parentRoute.id).toBe('/_authenticated')
    expect(usersRoute.parentRoute.id).toBe('/_authenticated')
    expect(invitationsRoute.parentRoute.id).toBe('/_authenticated')
    expect(rolesRoute.options.component).toBeTypeOf('function')
    expect(usersRoute.options.component).toBeTypeOf('function')
    expect(invitationsRoute.options.component).toBeTypeOf('function')
  })
})
