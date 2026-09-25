import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { toast } from 'sonner'
import { UsersPage } from './users-page'
import { ApiProblemError, type ApiClient } from '@/shared/api/client'
import { initializeI18n, i18n } from '@/shared/i18n'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
const account = { id: '019535d9-3df7-79fb-b466-fa907fa17f95', name: 'Lifecycle target', email: 'target@example.com', createdAt: '2026-01-01T00:00:00Z', authVersion: 7, roles: [], disabled: false }
function mount(overrides: Partial<ApiClient> = {}, canManage = true, actorID = 'other') {
 const api = { getUsers: vi.fn().mockResolvedValue({ users: [account] }), getRoles: vi.fn().mockResolvedValue({ roles: [] }), deactivateUser: vi.fn().mockResolvedValue(undefined), reactivateUser: vi.fn().mockResolvedValue(undefined), deleteUser: vi.fn().mockResolvedValue(undefined), ...overrides } as unknown as ApiClient
 const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
 render(<QueryClientProvider client={client}><UsersPage api={api} canManage={canManage} actorID={actorID} actorSuperAdmin /></QueryClientProvider>)
 return api
}
beforeEach(async () => {
 HTMLElement.prototype.hasPointerCapture ??= () => false
 HTMLElement.prototype.setPointerCapture ??= () => undefined
 HTMLElement.prototype.releasePointerCapture ??= () => undefined
 HTMLElement.prototype.scrollIntoView ??= () => undefined
 vi.clearAllMocks(); await initializeI18n(); await i18n.changeLanguage('en') })
it('requires matching email and confirmation, submits the displayed version, and announces success', async () => {
 const api = mount(); const user = userEvent.setup()
 await user.click(await screen.findByRole('button', { name: 'More actions' }))
 await user.click(screen.getByRole('menuitem', { name: 'Delete' }))
 const dialog = screen.getByRole('dialog')
 expect(within(dialog).getByText(/cannot be undone/)).toBeVisible()
 const submit = within(dialog).getByRole('button', { name: 'Delete' })
 expect(submit).toBeDisabled()
 await user.type(within(dialog).getByRole('textbox'), 'wrong@example.com'); expect(submit).toBeDisabled()
 await user.clear(within(dialog).getByRole('textbox')); await user.type(within(dialog).getByRole('textbox'), account.email)
 await user.click(submit)
 await waitFor(() => expect(api.deleteUser).toHaveBeenCalledExactlyOnceWith(account.id, 7))
 await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
 expect(toast.success).toHaveBeenCalled()
})
it('cancel has no side effects and self deletion/deactivation are disabled', async () => {
 const api = mount({}, true, account.id)
 await userEvent.setup().click(await screen.findByRole('button', { name: 'More actions' }))
 expect(screen.getByRole('menuitem', { name: 'Delete' })).toHaveAttribute('aria-disabled', 'true')
 expect(screen.getByRole('menuitem', { name: 'Deactivate' })).toHaveAttribute('aria-disabled', 'true')
 expect(api.deleteUser).not.toHaveBeenCalled()
})
it('supports disabled filtering and localized reactivation confirmation', async () => {
 await i18n.changeLanguage('zh-CN')
 const api = mount({ getUsers: vi.fn().mockResolvedValue({ users: [{ ...account, disabled: true }] }) })
 const user = userEvent.setup()
 await user.click(await screen.findByRole('button', { name: '更多操作' }))
 await user.click(screen.getByRole('menuitem', { name: '恢复' }))
 await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }))
 expect(api.reactivateUser).not.toHaveBeenCalled()
 await user.click(screen.getByRole('combobox'))
 await user.click(screen.getByRole('option', { name: '停用' }))
 await waitFor(() => expect(api.getUsers).toHaveBeenLastCalledWith(expect.objectContaining({ status: 'disabled', cursor: undefined }), expect.anything()))
 await user.click(await screen.findByRole('button', { name: '更多操作' }))
 await user.click(screen.getByRole('menuitem', { name: '恢复' }))
 await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '恢复' }))
 await waitFor(() => expect(api.reactivateUser).toHaveBeenCalledExactlyOnceWith(account.id, 7))
})
it('reports protected-target failures without a success message', async () => {
 const api = mount({ deactivateUser: vi.fn().mockRejectedValue(new ApiProblemError({ type: '/problems/last-super-admin', title: 'Last Super Admin', status: 409, code: 'last_super_admin' })) })
 const user = userEvent.setup()
 await user.click(await screen.findByRole('button', { name: 'More actions' }))
 await user.click(screen.getByRole('menuitem', { name: 'Deactivate' }))
 await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Deactivate' }))
 await waitFor(() => expect(toast.error).toHaveBeenCalled())
 expect(toast.success).not.toHaveBeenCalled(); expect(api.deactivateUser).toHaveBeenCalledExactlyOnceWith(account.id, 7)
})
it('read-only users can see status but not lifecycle actions', async () => {
 mount({}, false)
 expect(await screen.findByText('Active')).toBeVisible()
 expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
 expect(screen.queryByRole('button', { name: 'Deactivate' })).not.toBeInTheDocument()
})
