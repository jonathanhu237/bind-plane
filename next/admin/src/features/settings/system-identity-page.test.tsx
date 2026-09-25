import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { SystemIdentityPage } from './system-identity-page'
import type { ApiClient } from '@/shared/api/client'
import { i18n, initializeI18n } from '@/shared/i18n'

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

function renderPage(api: ApiClient, canWrite = true) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}><SystemIdentityPage api={api} canWrite={canWrite} /></QueryClientProvider>)
}

const savedIdentity = {
  systemName: 'Temvia',
  iconUrl: '/api/public/system-identity/icon?v=1',
  hasCustomIcon: false,
  revision: 1,
}

describe('system identity page', () => {
  beforeEach(async () => {
    await initializeI18n()
    await i18n.changeLanguage('en')
  })

  it('accepts 50 Unicode code points, including supplementary characters', async () => {
    const saveSystemIdentity = vi.fn().mockResolvedValue({ ...savedIdentity, systemName: '😀'.repeat(50) })
    const api = mockApi({ getSystemIdentity: vi.fn().mockResolvedValue(savedIdentity), saveSystemIdentity })
    const user = userEvent.setup()
    renderPage(api)

    const input = await screen.findByLabelText('System name')
    const value = '😀'.repeat(50)
    await user.clear(input)
    await user.type(input, value)

    expect(input).toHaveValue(value)
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(saveSystemIdentity).toHaveBeenCalledOnce())
    const form = saveSystemIdentity.mock.calls[0][0] as FormData
    expect(form.get('systemName')).toBe(value)
  })

  it('rejects the 51st Unicode code point without truncating the draft', async () => {
    const saveSystemIdentity = vi.fn()
    const api = mockApi({ getSystemIdentity: vi.fn().mockResolvedValue(savedIdentity), saveSystemIdentity })
    const user = userEvent.setup()
    renderPage(api)

    const input = await screen.findByLabelText('System name')
    const value = '😀'.repeat(51)
    await user.clear(input)
    await user.type(input, value)

    expect(input).toHaveValue(value)
    expect(screen.getByText('Use 50 characters or fewer.')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(saveSystemIdentity).not.toHaveBeenCalled()
  })

  it('keeps the identity visible but disables editing for a read-only administrator', async () => {
    const api = mockApi({ getSystemIdentity: vi.fn().mockResolvedValue(savedIdentity), saveSystemIdentity: vi.fn() })
    renderPage(api, false)

    expect(await screen.findByLabelText('System name')).toHaveValue('Temvia')
    expect(screen.getByLabelText('System name')).toBeDisabled()
    expect(screen.getAllByRole('textbox')).toHaveLength(1)
    expect(screen.getByLabelText('System icon')).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Restore default' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  })
})
