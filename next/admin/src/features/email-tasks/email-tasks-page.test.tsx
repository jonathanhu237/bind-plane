import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { EmailTasksPage } from './email-tasks-page'
import type { ApiClient } from '@/shared/api/client'
import { clearAccessDrafts } from '@/features/access/drafts'
import { i18n, initializeI18n } from '@/shared/i18n'

const ids = {
  failed: '00000000-0000-4000-8000-000000000101',
  sent: '00000000-0000-4000-8000-000000000102',
  attempt: '00000000-0000-4000-8000-000000000201',
}
const tasks = [
  { id: ids.failed, kind: 'password_reset' as const, recipientEmail: 'failed@example.com', recipientName: 'Failed', locale: 'en' as const, status: 'failed' as const, createdAt: '2025-01-02T03:04:05Z', availableAt: '2025-01-02T03:04:05Z', finishedAt: '2025-01-02T03:05:05Z', attemptCount: 2, round: 1, roundAttemptCount: 2, lastErrorCode: 'temporary', attempts: [{ id: ids.attempt, round: 1, attempt: 2, outcome: 'failed' as const, errorCode: 'temporary', occurredAt: '2025-01-02T03:05:05Z' }] },
  { id: ids.sent, kind: 'test_email' as const, recipientEmail: 'sent@example.com', recipientName: 'Sent', locale: 'zh-CN' as const, status: 'sent' as const, createdAt: '2025-01-01T03:04:05Z', availableAt: '2025-01-01T03:04:05Z', finishedAt: '2025-01-01T03:05:05Z', attemptCount: 1, round: 1, roundAttemptCount: 1 },
]

function mockApi(overrides: Partial<ApiClient> = {}): ApiClient {
  return { getSetupStatus: vi.fn(), setup: vi.fn(), login: vi.fn(), me: vi.fn(), logout: vi.fn(), requestPasswordReset: vi.fn(), completePasswordReset: vi.fn(), ...overrides }
}
function renderPage(api: ApiClient, canWrite = false) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}><EmailTasksPage api={api} userID="00000000-0000-4000-8000-000000000001" canWrite={canWrite} /></QueryClientProvider>)
}

describe('email tasks page', () => {
  beforeEach(async () => {
    clearAccessDrafts()
    await initializeI18n()
    await i18n.changeLanguage('en')
    HTMLElement.prototype.hasPointerCapture ??= () => false
    HTMLElement.prototype.setPointerCapture ??= () => undefined
    HTMLElement.prototype.releasePointerCapture ??= () => undefined
  })

  it('shows every supported state and keeps read-only actions unavailable', async () => {
    const getEmailTasks = vi.fn().mockResolvedValue({ tasks })
    const api = mockApi({ getEmailTasks })
    renderPage(api)

    expect(await screen.findByRole('heading', { name: 'Email tasks' })).toBeVisible()
    expect(await screen.findByText('failed@example.com')).toBeVisible()
    expect(await screen.findByText('sent@example.com')).toBeVisible()
    expect(screen.getByText('Failed')).toBeVisible()
    expect(screen.getByText('Sent by SMTP')).toBeVisible()
    expect(screen.queryByText('Waiting for retry')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Retry failed task' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete task' })).not.toBeInTheDocument()
    expect(getEmailTasks).toHaveBeenCalledWith(expect.objectContaining({ failedOnly: undefined }), expect.anything())
  })

  it('filters failed tasks and retries a selected task without exposing message content', async () => {
    const getEmailTasks = vi.fn().mockResolvedValue({ tasks: [tasks[0]] })
    const retryEmailTask = vi.fn()
    const retryEmailTasks = vi.fn().mockResolvedValue({ succeeded: 1, skipped: 0, failed: 0, items: [{ id: ids.failed, result: 'succeeded' as const }] })
    const getEmailTask = vi.fn().mockResolvedValue(tasks[0])
    const api = mockApi({ getEmailTasks, retryEmailTask, retryEmailTasks, getEmailTask })
    const user = userEvent.setup()
    renderPage(api, true)

    await screen.findByText('failed@example.com')
    await user.click(screen.getByRole('checkbox', { name: 'Select all' }))
    await user.click(screen.getByRole('button', { name: 'Retry selected' }))
    await waitFor(() => expect(retryEmailTasks).toHaveBeenCalledWith([ids.failed]))
    // Bulk retry is the write operation for a selection; the page must not
    // substitute a new task or reveal body/token fields in the detail dialog.
    await screen.findByText('failed@example.com')
    expect(screen.getByRole('button', { name: 'Retry selected' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'View details' }))
    const dialog = await screen.findByRole('dialog', { name: 'Details' })
    expect(within(dialog).getByText('failed@example.com')).toBeVisible()
    expect(within(dialog).queryByText(/token=|https?:\/\//i)).not.toBeInTheDocument()
  })
})
