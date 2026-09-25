import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { LoginForm } from './login-form'
import { PasswordResetForm } from './password-reset-form'
import { PasswordResetRequestForm } from './password-reset-request-form'
import { SetupForm } from './setup-form'
import { InvitationAcceptanceForm } from '@/features/access/invitation-acceptance-form'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { captureSetupAuthority, getSetupAuthority } from '@/shared/bootstrap/setup-authority'
import { i18n, initializeI18n } from '@/shared/i18n'

const token = '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ_abcdefghijklmnopqrstuvwxyz-'.slice(0, 43)

function renderWithQueryClient(element: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}>{element}</QueryClientProvider>)
}

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

describe('authentication forms', () => {
  beforeEach(async () => {
    await initializeI18n()
    await i18n.changeLanguage('en')
  })

  it('allows paste, exposes autocomplete semantics and sends normalized setup data', async () => {
    const user = userEvent.setup()
    const api = mockApi({ setup: vi.fn().mockResolvedValue(undefined) })
    const onSuccess = vi.fn()
    const view = renderWithQueryClient(<SetupForm api={api} token={token} onSuccess={onSuccess} />)

    const name = screen.getByLabelText('Name')
    const email = screen.getByLabelText('Email')
    const password = screen.getByLabelText('Password', { exact: true })
    const confirmation = screen.getByLabelText('Confirm password')
    expect(view.container.querySelectorAll('[data-slot="field-description"]')).toHaveLength(0)
    expect(name).not.toHaveAttribute('aria-describedby')
    expect(email).not.toHaveAttribute('aria-describedby')
    expect(password).not.toHaveAttribute('aria-describedby')
    expect(confirmation).not.toHaveAttribute('aria-describedby')
    expect(name).toHaveAttribute('autocomplete', 'name')
    expect(email).toHaveAttribute('autocomplete', 'email')
    expect(password).toHaveAttribute('autocomplete', 'new-password')
    await user.click(name)
    await user.paste('  Admin  ')
    await user.type(email, ' admin@example.com ')
    await user.type(password, 'Admin1!x')
    await user.type(confirmation, 'Admin1!x')
    await user.click(screen.getByRole('button', { name: 'Create administrator' }))

    await waitFor(() => expect(api.setup).toHaveBeenCalledWith({
      token,
      name: 'Admin',
      email: 'admin@example.com',
      password: 'Admin1!x',
      locale: 'en',
    }))
    expect(onSuccess).toHaveBeenCalledOnce()
  })

  it.each([
    {
      locale: 'en' as const,
      name: 'Name',
      email: 'Email',
      password: 'Password',
      confirmation: 'Confirm password',
      submit: 'Create administrator',
      messages: [
        'Enter a name between 1 and 100 characters without control characters.',
        'Enter a valid email address.',
        'Use a password between 8 and 128 characters with uppercase, lowercase, a number, and a special character.',
        'The passwords do not match.',
      ],
    },
    {
      locale: 'zh-CN' as const,
      name: '名称',
      email: '邮箱',
      password: '密码',
      confirmation: '确认密码',
      submit: '创建管理员',
      messages: [
        '请输入 1 到 100 个字符的名称，且不能包含控制字符。',
        '请输入有效的邮箱地址。',
        '请输入 8 到 128 个字符的密码，并至少包含大写字母、小写字母、数字和特殊符号。',
        '两次输入的密码不一致。',
      ],
    },
  ])('localizes setup client validation errors in $locale', async ({ locale, name, email, password, confirmation, submit, messages }) => {
    const user = userEvent.setup()
    await i18n.changeLanguage(locale)
    const view = renderWithQueryClient(<SetupForm api={mockApi()} token={token} onSuccess={vi.fn()} />)

    await user.type(screen.getByLabelText(confirmation), 'x')
    await user.click(screen.getByRole('button', { name: submit }))

    for (const message of messages) {
      expect(await screen.findByText(message)).toBeVisible()
    }
    expect(view.container.textContent).not.toMatch(/invalid_(?:name|email|password)|password_mismatch/)
    expect(screen.getByLabelText(name)).toHaveAttribute('aria-describedby', 'name-error')
    expect(screen.getByLabelText(email)).toHaveAttribute('aria-describedby', 'email-error')
    expect(screen.getByLabelText(password, { exact: true })).toHaveAttribute('aria-describedby', 'password-error')
    expect(screen.getByLabelText(confirmation)).toHaveAttribute('aria-describedby', 'passwordConfirmation-error')
  })

  it.each([
    ['en', 'Email', 'Password', 'Sign in', 'Enter a valid email address.', 'Enter your password.'],
    ['zh-CN', '邮箱', '密码', '登录', '请输入有效的邮箱地址。', '请输入密码。'],
  ] as const)('localizes login client validation errors in %s', async (locale, email, password, submit, emailMessage, passwordMessage) => {
    const user = userEvent.setup()
    await i18n.changeLanguage(locale)
    const view = renderWithQueryClient(<LoginForm api={mockApi()} onSuccess={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: submit }))

    expect(await screen.findByText(emailMessage)).toBeVisible()
    expect(await screen.findByText(passwordMessage)).toBeVisible()
    expect(screen.getByLabelText(email)).toHaveAttribute('aria-describedby', 'email-error')
    expect(screen.getByLabelText(password, { exact: true })).toHaveAttribute('aria-describedby', 'password-error')
    expect(view.container.textContent).not.toMatch(/invalid_(?:email|password|login_password)/)
  })

  it('waits for submission before showing login errors and distinguishes an overlong password', async () => {
    const user = userEvent.setup()
    const api = mockApi()
    renderWithQueryClient(<LoginForm api={api} onSuccess={vi.fn()} />)
    expect(screen.queryByText('Enter your password.')).not.toBeInTheDocument()
    await user.click(screen.getByLabelText('Password', { exact: true }))
    await user.tab()
    expect(screen.queryByText('Enter your password.')).not.toBeInTheDocument()
    await user.type(screen.getByLabelText('Email'), 'admin@example.com')
    await user.click(screen.getByLabelText('Password', { exact: true }))
    await user.paste('x'.repeat(129))
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('Password must not exceed 128 characters.')).toBeVisible()
    expect(screen.queryByText('Enter your password.')).not.toBeInTheDocument()
    expect(api.login).not.toHaveBeenCalled()
  })

  it('shows server field errors without a second form-level alert', async () => {
    const user = userEvent.setup()
    const api = mockApi({ login: vi.fn().mockRejectedValue(new ApiProblemError({
      type: '/problems/validation-failed', title: 'Validation failed', status: 422,
      code: 'validation_failed', errors: [{ pointer: '/email', code: 'invalid_email' }],
    })) })
    renderWithQueryClient(<LoginForm api={api} onSuccess={vi.fn()} />)
    await user.type(screen.getByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password', { exact: true }), 'password')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByText('Enter a valid email address.')).toBeVisible()
    expect(screen.queryByText('Check the highlighted fields and try again.')).not.toBeInTheDocument()
    expect(document.querySelector('[data-slot="alert"]')).toBeNull()
  })

  it('shows localized invalid credentials without exposing server diagnostics', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      login: vi.fn().mockRejectedValue(new Error('private diagnostic')),
    })
    renderWithQueryClient(<LoginForm api={api} onSuccess={vi.fn()} />)
    await user.type(screen.getByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password', { exact: true }), 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Something went wrong. Try again.')
    expect(screen.queryByText('private diagnostic')).not.toBeInTheDocument()
  })

  it('updates an existing server error when the language changes', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      login: vi.fn().mockRejectedValue(new ApiProblemError({
        type: '/problems/invalid-credentials',
        title: 'diagnostic',
        status: 401,
        code: 'invalid_credentials',
      })),
    })
    renderWithQueryClient(<LoginForm api={api} onSuccess={vi.fn()} />)

    const email = screen.getByLabelText('Email')
    const password = screen.getByLabelText('Password', { exact: true })
    await user.type(email, 'admin@example.com')
    await user.type(password, 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('The email or password is incorrect.')

    await i18n.changeLanguage('zh-CN')

    expect(await screen.findByRole('alert')).toHaveTextContent('邮箱或密码错误。')
    expect(screen.getByDisplayValue('admin@example.com')).toBe(email)
    expect(screen.getByDisplayValue('correct horse battery')).toBe(password)
  })

  it('uses the current language for a response that arrives after switching language', async () => {
    const user = userEvent.setup()
    let rejectRequest: (error: unknown) => void = () => undefined
    const api = mockApi({
      requestPasswordReset: vi.fn(() => new Promise<void>((_, reject) => { rejectRequest = reject })),
    })
    renderWithQueryClient(<PasswordResetRequestForm api={api} onAccepted={vi.fn()} />)

    const email = screen.getByLabelText('Email')
    await user.type(email, 'admin@example.com')
    await user.click(screen.getByRole('button', { name: 'Send reset link' }))
    await waitFor(() => expect(api.requestPasswordReset).toHaveBeenCalledOnce())

    await i18n.changeLanguage('zh-CN')
    rejectRequest(new ApiProblemError({
      type: '/problems/rate-limited',
      title: 'diagnostic',
      status: 429,
      code: 'rate_limited',
    }))

    expect(await screen.findByRole('alert')).toHaveTextContent('密码重置请求次数过多，请稍后重试。')
    expect(screen.getByDisplayValue('admin@example.com')).toBe(email)
    expect(api.requestPasswordReset).toHaveBeenCalledOnce()
  })

  it('updates invitation acceptance errors when the language changes', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      acceptInvitation: vi.fn().mockRejectedValue(new ApiProblemError({
        type: '/problems/validation-failed',
        title: 'diagnostic',
        status: 422,
        code: 'validation_failed',
        errors: [{ pointer: '/password', code: 'invalid_password' }],
      })),
    })
    renderWithQueryClient(<InvitationAcceptanceForm api={api} token="v1.token" onSuccess={vi.fn()} onInvalid={vi.fn()} />)

    const password = screen.getByLabelText('Password', { exact: true })
    await user.type(password, 'Aa1!xxxx')
    await user.type(screen.getByLabelText('Confirm password'), 'Aa1!xxxx')
    await user.click(screen.getByRole('button', { name: 'Accept invitation' }))
    expect(await screen.findByText('Review the highlighted fields and try again.')).toBeVisible()
    expect(await screen.findByText('Use a password between 8 and 128 characters with uppercase, lowercase, a number, and a special character.')).toBeVisible()

    await i18n.changeLanguage('zh-CN')

    expect(await screen.findByText('请检查标记出的字段后重试。')).toBeVisible()
    expect(await screen.findByText('请输入 8 到 128 个字符的密码，并至少包含大写字母、小写字母、数字和特殊符号。')).toBeVisible()
    expect(password).toHaveValue('Aa1!xxxx')
  })

  it('updates password reset errors when the language changes', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      completePasswordReset: vi.fn().mockRejectedValue(new ApiProblemError({
        type: '/problems/service-unavailable',
        title: 'diagnostic',
        status: 503,
        code: 'service_unavailable',
      })),
    })
    renderWithQueryClient(<PasswordResetForm api={api} token="v1.token" onSuccess={vi.fn()} onInvalidAuthority={vi.fn()} />)

    const password = screen.getByLabelText('Password', { exact: true })
    await user.type(password, 'Aa1!xxxx')
    await user.type(screen.getByLabelText('Confirm password'), 'Aa1!xxxx')
    await user.click(screen.getByRole('button', { name: 'Set new password' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('A required service is temporarily unavailable. Try again shortly.')

    await i18n.changeLanguage('zh-CN')

    expect(await screen.findByRole('alert')).toHaveTextContent('所需服务暂时不可用，请稍后重试。')
    expect(password).toHaveValue('Aa1!xxxx')
  })

  it('toggles password visibility with an accessible control', async () => {
    const user = userEvent.setup()
    const api = mockApi()
    renderWithQueryClient(<LoginForm api={api} onSuccess={vi.fn()} />)
    const password = screen.getByLabelText('Password', { exact: true })
    expect(password).toHaveAttribute('type', 'password')
    await user.click(screen.getByRole('button', { name: 'Show password' }))
    expect(password).toHaveAttribute('type', 'text')
    expect(screen.getByRole('button', { name: 'Hide password' })).toBeInTheDocument()
  })

  it('maps a setup-complete race to login and clears in-memory authority', async () => {
    const user = userEvent.setup()
    captureSetupAuthority(
      { pathname: '/setup', search: '', hash: `#token=${token}` },
      { state: null, replaceState: () => undefined },
    )
    const api = mockApi({
      setup: vi.fn().mockRejectedValue(new ApiProblemError({
        type: '/problems/setup-complete',
        title: 'Setup complete',
        status: 409,
        code: 'setup_complete',
      })),
    })
    const onSuccess = vi.fn()
    const onSetupComplete = vi.fn()
    renderWithQueryClient(<SetupForm api={api} token={token} onSuccess={onSuccess} onSetupComplete={onSetupComplete} />)

    await user.type(screen.getByLabelText('Name'), 'Admin')
    await user.type(screen.getByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password', { exact: true }), 'Admin1!x')
    await user.type(screen.getByLabelText('Confirm password'), 'Admin1!x')
    await user.click(screen.getByRole('button', { name: 'Create administrator' }))

    await waitFor(() => expect(onSetupComplete).toHaveBeenCalledOnce())
    expect(onSuccess).not.toHaveBeenCalled()
    expect(getSetupAuthority()).toBeUndefined()
  })

  it('applies a server field pointer and focuses the actionable control', async () => {
    const user = userEvent.setup()
    const api = mockApi({
      setup: vi.fn().mockRejectedValue(new ApiProblemError({
        type: '/problems/validation-failed',
        title: 'Validation failed',
        status: 422,
        code: 'validation_failed',
        errors: [{ pointer: '/email', code: 'invalid_email' }],
      })),
    })
    renderWithQueryClient(<SetupForm api={api} token={token} onSuccess={vi.fn()} />)

    await user.type(screen.getByLabelText('Name'), 'Admin')
    const email = screen.getByLabelText('Email')
    await user.type(email, 'admin@example.com')
    await user.type(screen.getByLabelText('Password', { exact: true }), 'Admin1!x')
    await user.type(screen.getByLabelText('Confirm password'), 'Admin1!x')
    await user.click(screen.getByRole('button', { name: 'Create administrator' }))

    expect(await screen.findByText('Enter a valid email address.')).toBeVisible()
    expect(email).toHaveAttribute('aria-describedby', 'email-error')
    await waitFor(() => expect(email).toHaveFocus())

    await i18n.changeLanguage('zh-CN')

    expect(await screen.findByText('请检查标记出的字段后重试。')).toBeVisible()
    expect(await screen.findByText('请输入有效的邮箱地址。')).toBeVisible()
    expect(screen.getByDisplayValue('admin@example.com')).toBe(email)
  })

  it.each([
    ['/problems/rate-limited', 429, 'rate_limited', 'Too many sign in attempts. Wait a moment and try again.'],
    ['/problems/service-unavailable', 503, 'service_unavailable', 'A required service is temporarily unavailable. Try again shortly.'],
  ])('keeps important login failures visible for %s', async (type, status, code, message) => {
    const user = userEvent.setup()
    const api = mockApi({ login: vi.fn().mockRejectedValue(new ApiProblemError({ type, title: 'diagnostic', status, code })) })
    renderWithQueryClient(<LoginForm api={api} onSuccess={vi.fn()} />)
    await user.type(screen.getByLabelText('Email'), 'admin@example.com')
    await user.type(screen.getByLabelText('Password', { exact: true }), 'correct horse battery')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent(message)
  })

  it('submits normalized recovery request and reports the generic accepted state', async () => {
    const user = userEvent.setup()
    const api = mockApi({ requestPasswordReset: vi.fn().mockResolvedValue(undefined) })
    const onAccepted = vi.fn()
    renderWithQueryClient(<PasswordResetRequestForm api={api} onAccepted={onAccepted} />)
    await user.type(screen.getByLabelText('Email'), ' admin@example.com ')
    await user.click(screen.getByRole('button', { name: 'Send reset link' }))
	await waitFor(() => expect(api.requestPasswordReset).toHaveBeenCalledWith({ email: 'admin@example.com' }))
    expect(onAccepted).toHaveBeenCalledOnce()
  })

  it('submits the reset token and password after matching NFC confirmation', async () => {
    const user = userEvent.setup()
    const api = mockApi({ completePasswordReset: vi.fn().mockResolvedValue(undefined) })
    const onSuccess = vi.fn()
    renderWithQueryClient(<PasswordResetForm api={api} token="v1.token" onSuccess={onSuccess} onInvalidAuthority={vi.fn()} />)
    await user.type(screen.getByLabelText('Password', { exact: true }), 'Aa1!e\u0301xxx')
    await user.type(screen.getByLabelText('Confirm password'), 'Aa1!éxxx')
    await user.click(screen.getByRole('button', { name: 'Set new password' }))
	await waitFor(() => expect(api.completePasswordReset).toHaveBeenCalledWith({ token: 'v1.token', password: 'Aa1!éxxx' }))
    expect(onSuccess).toHaveBeenCalledOnce()
  })
})
