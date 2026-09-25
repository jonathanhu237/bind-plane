import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AppRouterContext } from '@/app/context'
import type { ApiClient } from '@/shared/api/client'
import { captureSetupAuthority, clearSetupAuthority, getSetupAuthority } from '@/shared/bootstrap/setup-authority'
import { loadSetupRoute } from './setup'

const token = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz-'.slice(0, 43)

function mockApi(overrides: Partial<ApiClient> = {}): ApiClient {
  return {
    getSetupStatus: vi.fn(),
    setup: vi.fn(),
    login: vi.fn(),
    me: vi.fn(),
    logout: vi.fn(),
    requestPasswordReset: vi.fn(),
    completePasswordReset: vi.fn(),
    ...overrides,
  }
}

function context(api: ApiClient): AppRouterContext {
  return { api, queryClient: new QueryClient() }
}

function captureAuthority() {
  captureSetupAuthority(
    { pathname: '/setup', search: '', hash: `#token=${token}` },
    { state: null, replaceState: () => undefined },
  )
}

describe('setup route loader', () => {
  beforeEach(() => {
    clearSetupAuthority()
  })

  it('redirects without setup authority before requesting setup status', async () => {
    const api = mockApi({ getSetupStatus: vi.fn().mockResolvedValue({ status: 'required' }) })
    let failure: unknown

    try {
      await loadSetupRoute({ context: context(api) })
    } catch (error) {
      failure = error
    }

    expect(isRedirect(failure)).toBe(true)
    if (isRedirect(failure)) {
      expect(failure.options).toMatchObject({ to: '/login', replace: true })
    }
    expect(api.getSetupStatus).not.toHaveBeenCalled()
  })

  it('fetches setup status only after capturing authority', async () => {
    captureAuthority()
    const api = mockApi({ getSetupStatus: vi.fn().mockResolvedValue({ status: 'required' }) })

    await expect(loadSetupRoute({ context: context(api) })).resolves.toEqual({
      status: { status: 'required' },
      error: undefined,
    })
    expect(api.getSetupStatus).toHaveBeenCalledOnce()
    expect(getSetupAuthority()).toBe(token)
  })

  it('clears authority and redirects when setup is already complete', async () => {
    captureAuthority()
    const api = mockApi({ getSetupStatus: vi.fn().mockResolvedValue({ status: 'complete' }) })
    let failure: unknown

    try {
      await loadSetupRoute({ context: context(api) })
    } catch (error) {
      failure = error
    }

    expect(isRedirect(failure)).toBe(true)
    expect(getSetupAuthority()).toBeUndefined()
  })
})
