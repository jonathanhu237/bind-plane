import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, it, vi } from 'vitest'
import { OnlineUsersPage } from './online-users-page'
import type { ApiClient } from '@/shared/api/client'
import { i18n, initializeI18n } from '@/shared/i18n'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))
beforeEach(async () => { await initializeI18n(); await i18n.changeLanguage('en') })
const alice = { id: '019535d9-3df7-79fb-b466-fa907fa17f95', name: 'Alice', email: 'alice@example.com', lastSeenAt: '2026-09-08T00:00:00Z', sessionCount: 2 }
function mount(canManage: boolean, kickUser = vi.fn().mockResolvedValue(undefined)) {
  const api = { getOnlineUsers: vi.fn().mockResolvedValue({ users: [alice] }), kickUser } as unknown as ApiClient
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><OnlineUsersPage api={api} actorID="other" canManage={canManage} /></QueryClientProvider>)
  return kickUser
}
it('requires confirmation before signing out every device', async () => {
 const kick = mount(true)
 const user = userEvent.setup()
 await user.click(await screen.findByRole('button', { name: 'Force sign out' }))
 expect(kick).not.toHaveBeenCalled()
  expect(screen.getByText(/from all devices/)).toBeInTheDocument()
 await user.click(screen.getByRole('button', { name: 'Cancel' }))
 expect(kick).not.toHaveBeenCalled()
 await user.click(screen.getByRole('button', { name: 'Force sign out' }))
 const buttons = screen.getAllByRole('button', { name: 'Force sign out' })
 await user.click(buttons[buttons.length - 1])
 await waitFor(() => expect(kick).toHaveBeenCalledWith(alice.id))
})
it('keeps read-only users from accessing the kick action', async () => {
 mount(false)
 expect(await screen.findByText('alice@example.com')).toBeInTheDocument()
 expect(screen.queryByRole('button', { name: 'Force sign out' })).not.toBeInTheDocument()
})
it('keeps a failed operation open for retry', async () => {
 mount(true, vi.fn().mockRejectedValue(new Error('offline')))
 const user = userEvent.setup()
 await user.click(await screen.findByRole('button', { name: 'Force sign out' }))
 const buttons = screen.getAllByRole('button', { name: 'Force sign out' })
 await user.click(buttons[buttons.length - 1])
 await waitFor(() => expect(screen.getByRole('alertdialog')).toBeInTheDocument())
})
