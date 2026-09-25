import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { OperationLogsPage } from './operation-logs-page'
import type { ApiClient } from '@/shared/api/client'
import { i18n, initializeI18n } from '@/shared/i18n'

function mockApi(overrides: Partial<ApiClient> = {}): ApiClient {
  return {
    getSetupStatus: vi.fn(), setup: vi.fn(), login: vi.fn(), me: vi.fn(), logout: vi.fn(), requestPasswordReset: vi.fn(), completePasswordReset: vi.fn(),
    ...overrides,
  }
}

function renderWithQueryClient(element: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}>{element}</QueryClientProvider>)
}

describe('operation log filters', () => {
  beforeEach(async () => {
    await initializeI18n()
    await i18n.changeLanguage('en')
  })

  it('accepts a UUIDv7 actor filter and sends it to the API', async () => {
    const getOperationLogs = vi.fn().mockResolvedValue({ logs: [] })
    const api = mockApi({ getOperationLogs })
    const user = userEvent.setup()
    renderWithQueryClient(<OperationLogsPage api={api} userID="user-1" />)

    await screen.findByRole('heading', { name: 'Operation history' })
    const actor = screen.getByLabelText('Actor ID')
    const actorID = '019535d9-3df7-79fb-b466-fa907fa17f9e'
    await user.type(actor, actorID)
    await waitFor(() => expect(getOperationLogs).toHaveBeenLastCalledWith(expect.objectContaining({ actorId: actorID }), expect.anything()))
    expect(screen.queryByText('Enter a valid actor UUID.')).not.toBeInTheDocument()
  })

  it('shows invalid actor input instead of an endless loading state and makes no request', async () => {
    const getOperationLogs = vi.fn().mockResolvedValue({ logs: [] })
    const api = mockApi({ getOperationLogs })
    const user = userEvent.setup()
    renderWithQueryClient(<OperationLogsPage api={api} userID="user-1" />)

    await screen.findByRole('heading', { name: 'Operation history' })
    const initialCalls = getOperationLogs.mock.calls.length
    await user.type(screen.getByLabelText('Actor ID'), 'not-a-uuid')
    expect((await screen.findAllByText('Enter a valid actor UUID.'))[0]).toBeVisible()
    expect(screen.getByRole('status')).toBeVisible()
    expect(getOperationLogs).toHaveBeenCalledTimes(initialCalls)
  })

  it('keeps filters mounted while the log request fails', async () => {
    const getOperationLogs = vi.fn().mockRejectedValue(new Error('unavailable'))
    const api = mockApi({ getOperationLogs })
    renderWithQueryClient(<OperationLogsPage api={api} userID="user-1" />)

    expect(await screen.findByText('The API could not load operation history. Refresh the page to try again.')).toBeVisible()
    expect(screen.getByLabelText('Action')).toBeVisible()
    expect(screen.getByLabelText('Actor ID')).toBeVisible()
  })
})

const loginLog = {
  id: '019535d9-3df7-79fb-b466-fa907fa17f9e', actor: { name: 'Preview Admin', email: 'admin@example.com', kind: 'authenticated' },
  action: 'auth.login', objectType: 'session', result: 'success' as const, occurredAt: '2026-09-08T10:00:00Z', sourceIp: '192.0.2.1', details: { sessionCreated: true },
}

async function openLog(log: typeof loginLog | import('@/shared/api/contracts').OperationLog) {
  const api = mockApi({ getOperationLogs: vi.fn().mockResolvedValue({ logs: [log] }), getOperationLog: vi.fn().mockResolvedValue(log) })
  renderWithQueryClient(<OperationLogsPage api={api} userID="user-1" />)
  await userEvent.setup().click(await screen.findByRole('button', { name: 'View operation details' }))
  return screen.findByRole('dialog')
}

describe('readable operation details', () => {
  beforeEach(async () => { await initializeI18n(); await i18n.changeLanguage('en') })

  it('separates name and email and explains a login without irrelevant fields', async () => {
    await openLog(loginLog)
    expect(screen.getByText('Actor name', { selector: 'th' })).toBeVisible()
    expect(screen.getByText('Actor email', { selector: 'th' })).toBeVisible()
    expect(screen.getByText('Signed in successfully. A login session was created.')).toBeVisible()
    expect(screen.queryByText('Attempted account')).not.toBeInTheDocument()
    expect(screen.queryByText('Session created')).not.toBeInTheDocument()
    expect(screen.getByText('Technical details').parentElement).not.toHaveAttribute('open')
  })

  it('shows changed fields side by side without unchanged revision noise', async () => {
    await openLog({ ...loginLog, action: 'roles.update', objectType: 'role', details: { before: { name: 'Reader', description: 'Original', revision: 1 }, after: { name: 'Editor', description: 'Original', revision: 2 } } })
    expect(screen.getByRole('columnheader', { name: 'Before' })).toBeVisible()
    expect(screen.getByRole('columnheader', { name: 'After' })).toBeVisible()
    expect(screen.getByText('Reader')).toBeVisible()
    expect(screen.queryByText('Revision')).not.toBeInTheDocument()
    expect(screen.queryByText('Description')).not.toBeInTheDocument()
  })

  it('explains failed unverified attempts without treating the email as an identity', async () => {
    await openLog({ ...loginLog, actor: { kind: 'unverified' }, result: 'failure', attemptedAccount: 'attempt@example.com', details: { failure: 'invalid_credentials' } })
    expect(screen.getByText('The email or password was incorrect.')).toBeVisible()
    expect(screen.getAllByText('Unverified attempt')).toHaveLength(2)
    expect(screen.getByText('attempt@example.com')).toBeVisible()
  })

  it('keeps unknown actions readable and does not claim queued mail was delivered', async () => {
    await openLog({ ...loginLog, action: 'future.action', details: { mailQueued: true, futureField: 'Kept value' } })
    expect(screen.getAllByText('future.action').length).toBeGreaterThan(0)
    expect(screen.getByText('Queued for sending. This does not confirm delivery.')).toBeVisible()
    expect(screen.getByText('futureField')).toBeVisible()
    expect(screen.getByText('Kept value')).toBeVisible()
    expect(screen.queryByText('actionLabels.future_action')).not.toBeInTheDocument()
  })
})
