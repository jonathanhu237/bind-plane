import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { UsersPage } from './users-page'
import { generatePassword } from './password-generator'
import { ForcedPasswordChangeForm } from '@/features/auth/forced-password-change-form'
import { passwordMeetsRules } from '@/features/auth/schemas'
import type { ApiClient } from '@/shared/api/client'
import { initializeI18n, i18n } from '@/shared/i18n'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const role = { id: '019535d9-3df7-79fb-b466-fa907fa17f95', name: 'Operators', description: '', permissions: ['users.read'], revision: 1 }
const pending = { id: '019535d9-3df7-79fb-b466-fa907fa17f96', name: 'Fresh', email: 'fresh@example.com', createdAt: '2026-01-01T00:00:00Z', authVersion: 1, roles: [role], disabled: false, passwordChangeRequired: true }

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
}

beforeEach(async () => {
  HTMLElement.prototype.hasPointerCapture ??= () => false
  HTMLElement.prototype.setPointerCapture ??= () => undefined
  HTMLElement.prototype.releasePointerCapture ??= () => undefined
  HTMLElement.prototype.scrollIntoView ??= () => undefined
  vi.clearAllMocks()
  await initializeI18n()
  await i18n.changeLanguage('en')
})

it('generates passwords that always follow the rules', () => {
  const seen = new Set<string>()
  for (let index = 0; index < 300; index += 1) {
    const password = generatePassword()
    expect(password).toHaveLength(16)
    expect(passwordMeetsRules(password)).toBe(true)
    seen.add(password)
  }
  expect(seen.size).toBe(300)
})

it('creates an account with a generated password, shows it once, and marks accounts still on it', async () => {
  const createUser = vi.fn().mockImplementation(async (input: { name: string; email: string }) => ({ user: { ...pending, name: input.name, email: input.email } }))
  const api = { getUsers: vi.fn().mockResolvedValue({ users: [pending] }), getRoles: vi.fn().mockResolvedValue({ roles: [role] }), createUser } as unknown as ApiClient
  render(<QueryClientProvider client={client()}><UsersPage api={api} canManage actorSuperAdmin actorID="admin" /></QueryClientProvider>)
  expect(await screen.findByText('Initial password')).toBeVisible()
  const user = userEvent.setup()
  await user.click(screen.getByRole('button', { name: 'Create user' }))
  const dialog = screen.getByRole('dialog')
  const password = within(dialog).getByLabelText('Initial password') as HTMLInputElement
  expect(passwordMeetsRules(password.value)).toBe(true)
  await user.click(within(dialog).getByRole('button', { name: 'Create account' }))
  expect(within(dialog).getByText('Enter a name between 1 and 100 characters without control characters.')).toBeVisible()
  await user.type(within(dialog).getByLabelText('Name'), 'Grace')
  await user.type(within(dialog).getByLabelText('Email'), 'grace@example.com')
  await user.click(within(dialog).getByRole('checkbox', { name: 'Operators' }))
  expect(within(dialog).getByRole('checkbox', { name: 'Require a password change at first sign-in' })).toBeChecked()
  const chosen = password.value
  await user.click(within(dialog).getByRole('button', { name: 'Create account' }))
  await waitFor(() => expect(createUser).toHaveBeenCalledExactlyOnceWith({ name: 'Grace', email: 'grace@example.com', password: chosen, roleIds: [role.id], requirePasswordChange: true }))
  expect(await within(dialog).findByText('Account created')).toBeVisible()
  expect(within(dialog).getByText(chosen)).toBeVisible()
  expect(within(dialog).getByText('They will choose their own password at first sign-in.')).toBeVisible()
  await user.click(within(dialog).getByRole('button', { name: 'Done' }))
  await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
})

it('replaces the initial password and then signs in again', async () => {
  const changePersonalPassword = vi.fn().mockResolvedValue(undefined)
  const onChanged = vi.fn()
  render(<QueryClientProvider client={client()}><ForcedPasswordChangeForm api={{ changePersonalPassword } as unknown as ApiClient} onChanged={onChanged} /></QueryClientProvider>)
  const user = userEvent.setup()
  await user.type(screen.getByLabelText('Initial password'), 'Initial1!pass')
  await user.type(screen.getByLabelText('New password'), 'Chosen2@pass')
  await user.type(screen.getByLabelText('Confirm new password'), 'Chosen2@different')
  await user.click(screen.getByRole('button', { name: 'Change password' }))
  expect(changePersonalPassword).not.toHaveBeenCalled()
  await user.clear(screen.getByLabelText('Confirm new password'))
  await user.type(screen.getByLabelText('Confirm new password'), 'Chosen2@pass')
  await user.click(screen.getByRole('button', { name: 'Change password' }))
  await waitFor(() => expect(changePersonalPassword).toHaveBeenCalledExactlyOnceWith({ currentPassword: 'Initial1!pass', newPassword: 'Chosen2@pass', confirmPassword: 'Chosen2@pass' }))
  expect(onChanged).toHaveBeenCalledOnce()
})
