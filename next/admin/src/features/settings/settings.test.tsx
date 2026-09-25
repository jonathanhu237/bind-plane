import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { EmailSettingsPage } from './email-settings-page'
import { OperationLogRetentionCard } from './operation-log-retention-card'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { clearAccessDrafts } from '@/features/access/drafts'
import { i18n, initializeI18n } from '@/shared/i18n'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

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

function renderWithQueryClient(element: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}>{element}</QueryClientProvider>)
}

describe('email settings page', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    HTMLElement.prototype.hasPointerCapture ??= () => false
    HTMLElement.prototype.setPointerCapture ??= () => undefined
    HTMLElement.prototype.releasePointerCapture ??= () => undefined
    HTMLElement.prototype.scrollIntoView ??= () => undefined
    await initializeI18n()
    await i18n.changeLanguage('en')
    clearAccessDrafts()
  })

  it('requires an explicit language on the first save and lets the administrator choose the test recipient', async () => {
    const saveEmailSettings = vi.fn().mockResolvedValue({ configured: true, host: 'mailpit', port: 587, security: 'starttls', passwordSet: false, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 1, autoRetryCount: 9, retentionDays: 30 })
    const testEmailSettings = vi.fn().mockResolvedValue({ status: 'accepted' })
    const api = mockApi({
      getEmailSettings: vi.fn().mockResolvedValueOnce({ configured: false, passwordSet: false, revision: 0 }).mockResolvedValue({ configured: true, host: 'mailpit', port: 587, security: 'starttls', passwordSet: false, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 1, autoRetryCount: 9, retentionDays: 30 }),
      saveEmailSettings,
      testEmailSettings,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<EmailSettingsPage api={api} />)

    expect(await screen.findByRole('heading', { name: 'System settings' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.getByText('Choose a language')).toBeVisible()
    expect(screen.queryByLabelText('Recipient email')).not.toBeInTheDocument()

    await user.type(screen.getByLabelText('SMTP host'), 'mailpit')
    await user.type(screen.getByLabelText('From address'), 'no-reply@example.com')
    await user.type(screen.getByLabelText('From name'), 'Temvia')
    await user.click(screen.getByRole('combobox', { name: 'Default email language' }))
    await user.click(await screen.findByRole('option', { name: 'English' }))
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()

    await user.click(screen.getByRole('button', { name: 'Send test email' }))
    expect(toast.error).toHaveBeenCalledWith('Save email settings first', { description: 'Save your SMTP changes before submitting a test email.' })
    expect(testEmailSettings).not.toHaveBeenCalled()
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(saveEmailSettings).toHaveBeenCalledOnce())
    await user.click(screen.getByRole('button', { name: 'Send test email' }))
    const dialog = await screen.findByRole('dialog', { name: 'Send test email' })
    await user.type(within(dialog).getByLabelText('Recipient email'), 'real@example.com')
    await user.click(within(dialog).getByRole('button', { name: 'Send test email' }))
    await waitFor(() => expect(testEmailSettings).toHaveBeenCalledWith({ recipient: 'real@example.com' }))
    expect(toast.success).toHaveBeenCalledWith('Test email submitted. Delivery is still in progress.')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(saveEmailSettings).toHaveBeenCalledOnce()
  })

  it('shows a failure toast with the localized delivery problem', async () => {
    const testEmailSettings = vi.fn().mockRejectedValue(new ApiProblemError({ type: '/problems/mail-not-configured', title: 'mail unavailable', status: 503, code: 'mail_not_configured' }))
    const api = mockApi({
      getEmailSettings: vi.fn().mockResolvedValue({ configured: true, host: 'smtp.example.com', port: 587, security: 'starttls', passwordSet: false, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 1, autoRetryCount: 9, retentionDays: 30 }),
      testEmailSettings,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<EmailSettingsPage api={api} />)

    await screen.findByRole('heading', { name: 'System settings' })
    await user.click(screen.getByRole('button', { name: 'Send test email' }))
    const dialog = await screen.findByRole('dialog', { name: 'Send test email' })
    await user.type(within(dialog).getByLabelText('Recipient email'), 'real@example.com')
    await user.click(within(dialog).getByRole('button', { name: 'Send test email' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Test email could not be submitted.', { description: 'Email service is not configured. Contact an administrator.' }))
  })

  it('requires a valid recipient before sending a test', async () => {
    const testEmailSettings = vi.fn().mockResolvedValue({ status: 'accepted' })
    const api = mockApi({
      getEmailSettings: vi.fn().mockResolvedValue({ configured: true, host: 'smtp.example.com', port: 587, security: 'starttls', passwordSet: false, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 1, autoRetryCount: 9, retentionDays: 30 }),
      testEmailSettings,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<EmailSettingsPage api={api} />)

    await screen.findByRole('heading', { name: 'System settings' })
    await user.click(screen.getByRole('button', { name: 'Send test email' }))
    const dialog = await screen.findByRole('dialog', { name: 'Send test email' })
    await user.type(within(dialog).getByLabelText('Recipient email'), 'not-an-email')
    await user.click(within(dialog).getByRole('button', { name: 'Send test email' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Enter a valid email address.')
    expect(testEmailSettings).not.toHaveBeenCalled()
  })

  it('announces that email settings were saved after confirmation', async () => {
    const saveEmailSettings = vi.fn().mockResolvedValue({ configured: true, host: 'smtp.example.com', port: 587, security: 'starttls', passwordSet: false, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 1 })
    const api = mockApi({
      getEmailSettings: vi.fn().mockResolvedValue({ configured: false, passwordSet: false, revision: 0 }),
      saveEmailSettings,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<EmailSettingsPage api={api} />)

    await screen.findByRole('heading', { name: 'System settings' })
    await user.click(screen.getByRole('combobox', { name: 'Default email language' }))
    await user.click(await screen.findByRole('option', { name: 'English' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveEmailSettings).toHaveBeenCalledOnce())
    expect(toast.success).toHaveBeenCalledWith('Email settings saved.')
  })

  it('explains an identity-bound password failure and retries with replacement credentials', async () => {
    const saveEmailSettings = vi.fn()
      .mockRejectedValueOnce(new ApiProblemError({ type: '/problems/validation-failed', title: 'validation failed', status: 422, code: 'invalid_mail_settings' }))
      .mockResolvedValueOnce({ configured: true, host: 'smtp-new.example.com', port: 587, security: 'starttls', username: 'mailer', passwordSet: true, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 4 })
    const api = mockApi({
      getEmailSettings: vi.fn().mockResolvedValue({ configured: true, host: 'smtp-old.example.com', port: 587, security: 'starttls', username: 'mailer', passwordSet: true, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 3 }),
      saveEmailSettings,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<EmailSettingsPage api={api} />)

    await screen.findByRole('heading', { name: 'System settings' })
    const host = screen.getByLabelText('SMTP host')
    await user.clear(host)
    await user.type(host, 'smtp-new.example.com')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveEmailSettings).toHaveBeenNthCalledWith(1, expect.objectContaining({ host: 'smtp-new.example.com', username: 'mailer', password: undefined, revision: 3 })))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Email settings could not be saved.', { description: expect.stringContaining('enter a new password') }))
    const replacement = screen.getByLabelText('Replace password')
    expect(replacement).toBeEnabled()
    await user.type(replacement, 'replacement-secret')
    await user.click(screen.getByRole('button', { name: 'Save' }))

    await waitFor(() => expect(saveEmailSettings).toHaveBeenNthCalledWith(2, expect.objectContaining({ host: 'smtp-new.example.com', username: 'mailer', password: 'replacement-secret', revision: 3 })))
    expect(toast.success).toHaveBeenCalledWith('Email settings saved.')
  })

  it('keeps fields and mutation controls unavailable to settings readers', async () => {
    const api = mockApi({
      getEmailSettings: vi.fn().mockResolvedValue({ configured: true, host: 'smtp.example.com', port: 587, security: 'starttls', username: 'mailer', passwordSet: true, fromAddress: 'no-reply@example.com', fromName: 'Temvia', defaultLocale: 'en', revision: 3 }),
    })
    renderWithQueryClient(<EmailSettingsPage api={api} canWrite={false} />)

    const heading = await screen.findByRole('heading', { name: 'System settings' })
    expect(heading).toBeVisible()
    expect(screen.getByLabelText('SMTP host')).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Send test email' })).not.toBeInTheDocument()
    expect(within(screen.getByRole('group', { name: 'Authentication' })).getByLabelText('Use SMTP authentication')).toBeDisabled()
  })

  it('keeps the draft on a conflict and requires a browser refresh', async () => {
    const saveEmailSettings = vi.fn().mockRejectedValue(new ApiProblemError({ type: '/problems/stale-revision', title: 'stale revision', status: 409, code: 'stale_revision' }))
    const getEmailSettings = vi.fn().mockResolvedValue({ configured: false, passwordSet: false, revision: 0 })
    const api = mockApi({ getEmailSettings, saveEmailSettings })
    const user = userEvent.setup()
    renderWithQueryClient(<EmailSettingsPage api={api} />)

    await screen.findByRole('heading', { name: 'System settings' })
    await user.click(screen.getByRole('combobox', { name: 'Default email language' }))
    await user.click(await screen.findByRole('option', { name: 'English' }))
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(saveEmailSettings).toHaveBeenCalledOnce())
    expect(screen.getByLabelText('SMTP host')).toHaveValue('')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Discard draft and reload' })).not.toBeInTheDocument()
    expect(toast.error).toHaveBeenCalledWith('This record changed', { description: 'Another administrator changed this record. Refresh the page before trying again. Refreshing will discard unsaved changes.' })
  })
})

describe('operation log retention settings', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    await initializeI18n()
    await i18n.changeLanguage('en')
    clearAccessDrafts()
  })

  it('keeps an invalid value editable until it is corrected', async () => {
    const saveOperationLogRetention = vi.fn().mockResolvedValue({ retentionDays: 30, revision: 2 })
    const api = mockApi({
      getOperationLogRetention: vi.fn().mockResolvedValue({ retentionDays: 180, revision: 1 }),
      saveOperationLogRetention,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<OperationLogRetentionCard api={api} userID="user-1" />)

    const input = await screen.findByLabelText('Retention days')
    await waitFor(() => expect(input).toBeEnabled())
    await user.clear(input)
    await user.type(input, '0')
    expect(input).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    await user.clear(input)
    await user.type(input, '30')
    expect(input).toHaveAttribute('aria-invalid', 'false')
    expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(saveOperationLogRetention).toHaveBeenCalledWith({ retentionDays: 30, revision: 1 }))
  })

  it('does not enable editing or saving before the authoritative read succeeds', async () => {
    const api = mockApi({
      getOperationLogRetention: vi.fn().mockRejectedValue(new ApiProblemError({ type: '/problems/dependency-unavailable', title: 'unavailable', status: 503 })),
      saveOperationLogRetention: vi.fn(),
    })
    renderWithQueryClient(<OperationLogRetentionCard api={api} userID="user-1" />)

    const input = await screen.findByLabelText('Retention days')
    expect(input).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(await screen.findByText('The API could not load the retention setting. Refresh the page to try again.')).toBeVisible()
  })

  it('reloads the authoritative revision before discarding a conflict and saving again', async () => {
    const saveOperationLogRetention = vi.fn()
      .mockRejectedValueOnce(new ApiProblemError({ type: '/problems/stale-revision', title: 'stale', status: 409, code: 'stale_revision' }))
      .mockResolvedValueOnce({ retentionDays: 90, revision: 6 })
    const getOperationLogRetention = vi.fn()
      .mockResolvedValueOnce({ retentionDays: 180, revision: 4 })
      .mockResolvedValueOnce({ retentionDays: 120, revision: 5 })
      .mockResolvedValue({ retentionDays: 90, revision: 6 })
    const api = mockApi({
      getOperationLogRetention,
      saveOperationLogRetention,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<OperationLogRetentionCard api={api} userID="user-1" />)

    const input = await screen.findByLabelText('Retention days')
    await waitFor(() => expect(input).toBeEnabled())
    await user.clear(input)
    await user.type(input, '90')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Discard draft and reload' })).toBeVisible())
    expect(input).toBeEnabled()
    await user.click(screen.getByRole('button', { name: 'Discard draft and reload' }))
    await waitFor(() => expect(input).toHaveValue(120))
    expect(getOperationLogRetention).toHaveBeenCalledTimes(2)
    expect(screen.queryByRole('button', { name: 'Discard draft and reload' })).not.toBeInTheDocument()
    await user.clear(input)
    await user.type(input, '90')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(saveOperationLogRetention).toHaveBeenLastCalledWith({ retentionDays: 90, revision: 5 }))
  })

  it('keeps the conflict and save disabled when the authoritative reload fails', async () => {
    const stale = new ApiProblemError({ type: '/problems/stale-revision', title: 'stale', status: 409, code: 'stale_revision' })
    const getOperationLogRetention = vi.fn()
      .mockResolvedValueOnce({ retentionDays: 180, revision: 4 })
      .mockRejectedValueOnce(new ApiProblemError({ type: '/problems/dependency-unavailable', title: 'unavailable', status: 503 }))
    const saveOperationLogRetention = vi.fn().mockRejectedValue(stale)
    const api = mockApi({ getOperationLogRetention, saveOperationLogRetention })
    const user = userEvent.setup()
    renderWithQueryClient(<OperationLogRetentionCard api={api} userID="user-1" />)

    const input = await screen.findByLabelText('Retention days')
    await waitFor(() => expect(input).toBeEnabled())
    await user.clear(input)
    await user.type(input, '90')
    await user.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(screen.getByRole('button', { name: 'Discard draft and reload' })).toBeVisible())
    await user.click(screen.getByRole('button', { name: 'Discard draft and reload' }))

    await waitFor(() => expect(getOperationLogRetention).toHaveBeenCalledTimes(2))
    expect(screen.getByRole('button', { name: 'Save' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Discard draft and reload' })).toBeVisible()
    expect(saveOperationLogRetention).toHaveBeenCalledOnce()
  })
})
