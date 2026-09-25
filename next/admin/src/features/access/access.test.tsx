import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { RolesPage } from './roles-page'
import { UsersPage } from './users-page'
import { InvitationsPage } from './invitations-page'
import { AccessDenied, AccessError } from './access-error'
import { clearAccessDrafts } from './drafts'
import { ApiProblemError, ApiTransportError, type ApiClient } from '@/shared/api/client'
import type { Invitation, Permission, Role } from '@/shared/api/contracts'
import { i18n, initializeI18n } from '@/shared/i18n'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const usersRole: Role = {
  id: '019535d9-3df7-79fb-b466-fa907fa17f95',
  name: 'Users reader',
  description: 'Read users',
  permissions: ['users.read'],
  revision: 1,
  assignmentCount: 1,
}

const rolesRole: Role = {
  id: '019535d9-3df7-79fb-b466-fa907fa17f96',
  name: 'Roles reader',
  description: 'Read roles',
  permissions: ['roles.read'],
  revision: 1,
  assignmentCount: 0,
}

const systemRole: Role = {
  id: '019535d9-3df7-79fb-b466-fa907fa17f94',
  name: 'Super Admin',
  description: '',
  system: 'super_admin',
  permissions: ['roles.read', 'users.read'],
  revision: 1,
  assignmentCount: 1,
}

const permissionDefinitions: Permission[] = [
  { key: 'users.read', resource: 'users', action: 'read', labelKey: 'permissions.users.read', description: 'View users' },
  { key: 'roles.read', resource: 'roles', action: 'read', labelKey: 'permissions.roles.read', description: 'View roles' },
]

const invitationPermissionDefinitions: Permission[] = [
  ...permissionDefinitions,
  { key: 'invitations.read', resource: 'invitations', action: 'read', labelKey: 'permissions.invitations.read', description: 'View invitations' },
  { key: 'invitations.write', resource: 'invitations', action: 'write', labelKey: 'permissions.invitations.write', description: 'Create and manage invitations' },
]

const pendingInvitation: Invitation = {
  id: '019535d9-3df7-79fb-b466-fa907fa17f97',
  name: 'Lin',
  email: 'lin@example.com',
  locale: 'zh-CN',
  roles: [usersRole],
  expiresAt: '2099-01-02T00:00:00Z',
  createdAt: '2026-09-03T00:00:00Z',
  revision: 1,
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

function renderWithQueryClient(element: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return render(<QueryClientProvider client={queryClient}>{element}</QueryClientProvider>)
}

function problem(type: string, status: number, code?: string) {
  return new ApiProblemError({ type, title: 'diagnostic', status, ...(code ? { code } : {}) })
}

describe('access components', () => {
  beforeEach(async () => {
    vi.clearAllMocks()
    await initializeI18n()
    await i18n.changeLanguage('en')
    clearAccessDrafts()
  })

  it('groups the role permission picker by resource', async () => {
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole, usersRole, rolesRole], permissions: permissionDefinitions }),
    })
    renderWithQueryClient(<RolesPage api={api} canManage />)

    await screen.findByRole('heading', { name: 'Role management' })
    await userEvent.setup().click(screen.getByRole('button', { name: 'Create role' }))

    const dialog = await screen.findByRole('dialog', { name: 'Create role' })
    await waitFor(() => expect(dialog.querySelectorAll('fieldset > legend')).toHaveLength(3))
    const legends = Array.from(dialog.querySelectorAll('fieldset > legend')).map((legend) => legend.textContent)
    expect(legends).toEqual(['Permissions', 'Roles', 'Users'])
    expect(within(dialog).getByRole('checkbox', { name: /View users/ })).toBeVisible()
    expect(within(dialog).getByRole('checkbox', { name: /View roles/ })).toBeVisible()
  })

  it('sorts role rows through the data table', async () => {
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [usersRole, systemRole, rolesRole], permissions: permissionDefinitions }),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage={false} />)

    const table = await screen.findByRole('table')
    await user.click(within(table).getByRole('button', { name: 'Role name' }))
    const rows = within(table).getAllByRole('row')
    expect(rows[1]).toHaveTextContent('Roles reader')
    expect(rows[2]).toHaveTextContent('Super Admin')
    expect(rows[3]).toHaveTextContent('Users reader')
  })

  it('announces a failed role read once and asks for a manual refresh', async () => {
    const api = mockApi({ getRoles: vi.fn().mockRejectedValue(new ApiTransportError('network')) })
    renderWithQueryClient(<RolesPage api={api} canManage={false} />)

    expect(await screen.findByText('Refresh the page to load the latest data.')).toBeVisible()
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Access data is unavailable', { description: 'The API could not load access data. Refresh the page to try again.' }))
    expect(toast.error).toHaveBeenCalledOnce()
  })

  it('announces successful role creation after the API confirms it', async () => {
    const createdRole: Role = { id: '019535d9-3df7-79fb-b466-fa907fa17f98', name: 'Auditor', description: '', permissions: ['users.read'], revision: 1, assignmentCount: 0 }
    const createRole = vi.fn().mockResolvedValue(createdRole)
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole], permissions: permissionDefinitions }),
      createRole,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage />)

    await screen.findByRole('heading', { name: 'Role management' })
    await user.click(screen.getByRole('button', { name: 'Create role' }))
    const editor = await screen.findByRole('dialog', { name: 'Create role' })
    await user.type(within(editor).getByLabelText('Role name'), 'Auditor')
    await user.click(within(editor).getByRole('checkbox', { name: 'View users' }))
    await user.click(within(editor).getByRole('button', { name: 'Save role' }))

    await waitFor(() => expect(createRole).toHaveBeenCalledWith({ name: 'Auditor', description: '', permissions: ['users.read'] }))
    expect(toast.success).toHaveBeenCalledWith('Role created.')
  })

  it('renders a read-only users page without loading role administration data', async () => {
    const api = mockApi({
      getUsers: vi.fn().mockResolvedValue({ users: [{ id: '019535d9-3df7-79fb-b466-fa907fa17f91', name: 'Ada', email: 'ada@example.com', createdAt: '2026-09-02T00:00:00Z', authVersion: 1, roles: [usersRole] }] }),
      getRoles: vi.fn(),
    })
    renderWithQueryClient(<UsersPage api={api} canManage={false} />)

    expect(await screen.findByText('Ada')).toBeVisible()
    expect(api.getRoles).not.toHaveBeenCalled()
    expect(screen.queryByRole('button', { name: 'Invite user' })).not.toBeInTheDocument()
    expect(screen.queryByRole('checkbox')).not.toBeInTheDocument()
  })

  it('sorts users by role without offering a role filter', async () => {
    const getUsers = vi.fn().mockResolvedValue({ users: [{ id: '019535d9-3df7-79fb-b466-fa907fa17f91', name: 'Ada', email: 'ada@example.com', createdAt: '2026-09-02T00:00:00Z', authVersion: 1, roles: [usersRole] }] })
    const api = mockApi({ getUsers })
    const user = userEvent.setup()
    renderWithQueryClient(<UsersPage api={api} canManage={false} />)

    const table = await screen.findByRole('table')
    expect(screen.queryByRole('combobox', { name: 'Filter by role' })).not.toBeInTheDocument()

    await user.click(within(table).getByRole('button', { name: 'Role' }))
    await waitFor(() => expect(getUsers).toHaveBeenLastCalledWith(
      { cursor: undefined, q: undefined, roleId: undefined, sort: 'roles', direction: 'asc' },
      expect.anything(),
    ))
  })

  it('keeps invitations on their own page and confirms the system mail language', async () => {
    const resendInvitation = vi.fn().mockResolvedValue({ invitation: pendingInvitation })
    const api = mockApi({
      getInvitations: vi.fn().mockResolvedValue({ invitations: [pendingInvitation] }),
      getRoles: vi.fn().mockResolvedValue({ roles: [usersRole], permissions: permissionDefinitions }),
      resendInvitation,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<InvitationsPage api={api} canManage actorSuperAdmin />)

    const table = await screen.findByRole('table')
    expect(within(table).getByText('Lin')).toBeVisible()
    expect(within(table).getByText('Pending')).toBeVisible()
    expect(within(table).getByText(/Expires Jan 2, 2099/)).toBeVisible()
    expect(within(table).queryByRole('columnheader', { name: 'Email language' })).not.toBeInTheDocument()
    expect(within(table).queryByRole('columnheader', { name: 'Created' })).not.toBeInTheDocument()

    await user.click(within(table).getByRole('button', { name: 'Resend' }))
    const confirmation = await screen.findByRole('alertdialog')
    expect(confirmation).toHaveTextContent('previous link will stop working')
    await user.click(within(confirmation).getByRole('button', { name: 'Resend' }))
    await waitFor(() => expect(resendInvitation).toHaveBeenCalledWith(pendingInvitation.id))
    expect(toast.success).toHaveBeenCalledWith('Invitation email submitted.')
  })

  it('shows role details without type labels and localizes permissions', async () => {
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole, usersRole], permissions: permissionDefinitions }),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage={false} />)

    const table = await screen.findByRole('table')
    expect(within(table).queryByRole('columnheader', { name: 'Type' })).not.toBeInTheDocument()
    expect(within(table).getByRole('img', { name: 'Temvia built-in role; it cannot be edited or deleted.' })).toBeVisible()
    await user.click(within(table).getByRole('button', { name: /Super Admin/ }))
    const detail = await screen.findByRole('dialog', { name: 'Super Admin' })
    expect(detail).not.toHaveTextContent('Built-in role')
    expect(detail).toHaveTextContent('View users')
    expect(detail).not.toHaveTextContent('users.read')
  })

  it('explains why an assigned role cannot be deleted', async () => {
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [usersRole], permissions: permissionDefinitions }),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage />)

    const row = (await screen.findByText('Users reader')).closest('tr')
    expect(row).not.toBeNull()
    await user.click(within(row!).getByRole('button', { name: 'More actions' }))
    const deleteItem = await screen.findByRole('menuitem', { name: 'Delete' })
    expect(deleteItem).toHaveAttribute('aria-disabled', 'true')
    expect(deleteItem).toHaveAttribute('title', 'Reassign all users and invitations before deleting this role.')
  })

  it('selects and locks the permissions required by an invitation feature', async () => {
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole], permissions: invitationPermissionDefinitions, combinations: [{ key: 'invitations.create', labelKey: 'permissions.combinations.invitationsCreate', description: 'Create invitations', permissions: ['invitations.write', 'roles.read'], trigger: ['invitations.write'] }, { key: 'invitations.manage', labelKey: 'permissions.combinations.invitationsManage', description: 'View and manage invitations', permissions: ['invitations.read', 'invitations.write', 'roles.read'], trigger: ['invitations.read', 'invitations.write'] }] }),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage />)

    await screen.findByRole('heading', { name: 'Role management' })
    await user.click(screen.getByRole('button', { name: 'Create role' }))
    const editor = await screen.findByRole('dialog', { name: 'Create role' })
    const manage = within(editor).getByRole('checkbox', { name: 'Manage invitations' })
    await user.click(manage)
    expect(manage).toBeChecked()
    expect(within(editor).getByRole('checkbox', { name: 'View invitations' })).not.toBeChecked()
    expect(within(editor).getByRole('checkbox', { name: 'View roles' })).toBeChecked()
    expect(within(editor).getByRole('checkbox', { name: 'View invitations' })).not.toBeDisabled()
    expect(within(editor).getByRole('checkbox', { name: 'View roles' })).toBeDisabled()

    await user.click(within(editor).getByRole('checkbox', { name: 'View invitations' }))
    expect(within(editor).getByRole('checkbox', { name: 'View invitations' })).toBeChecked()
    expect(within(editor).getByRole('checkbox', { name: 'View roles' })).toBeChecked()
    expect(within(editor).getByRole('checkbox', { name: 'View invitations' })).not.toBeDisabled()

    await user.click(manage)
    expect(within(editor).getByRole('checkbox', { name: 'View invitations' })).toBeChecked()
  })

  it('disables invitation actions when the selected role exceeds the actor permissions', async () => {
    const api = mockApi({
      getInvitations: vi.fn().mockResolvedValue({ invitations: [{ ...pendingInvitation, roles: [systemRole] }] }),
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole], permissions: permissionDefinitions }),
    })
    renderWithQueryClient(<InvitationsPage api={api} canManage actorPermissions={['invitations.write', 'invitations.read', 'roles.read']} />)

    const table = await screen.findByRole('table')
    const resend = within(table).getByRole('button', { name: /Resend: You need higher permissions/ })
    const revoke = within(table).getByRole('button', { name: /Revoke: You need higher permissions/ })
    expect(resend).toBeDisabled()
    expect(revoke).toBeDisabled()
  })

  it('requires explicit invitation role selection instead of defaulting to Super Admin', async () => {
    const api = mockApi({
      getUsers: vi.fn().mockResolvedValue({ users: [{ id: '019535d9-3df7-79fb-b466-fa907fa17f91', name: 'Ada', email: 'ada@example.com', createdAt: '2026-09-02T00:00:00Z', authVersion: 1, roles: [usersRole] }] }),
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole, usersRole], permissions: permissionDefinitions }),
      getInvitations: vi.fn().mockResolvedValue({ invitations: [] }),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<InvitationsPage api={api} canManage actorSuperAdmin />)

    await screen.findByRole('heading', { name: 'Invitation management' })
    await user.click(screen.getByRole('button', { name: 'Invite user' }))
    const inviteForm = screen.getByLabelText('Name').closest('form')
    expect(inviteForm).not.toBeNull()
    expect(within(inviteForm!).getByRole('checkbox', { name: 'Super Admin' })).not.toBeChecked()
    expect(within(inviteForm!).getByRole('checkbox', { name: 'Users reader' })).not.toBeChecked()
  })

  it('updates an existing invitation validation message when the language changes', async () => {
    const api = mockApi({
      getUsers: vi.fn().mockResolvedValue({ users: [{ id: '019535d9-3df7-79fb-b466-fa907fa17f91', name: 'Ada', email: 'ada@example.com', createdAt: '2026-09-02T00:00:00Z', authVersion: 1, roles: [usersRole] }] }),
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole, usersRole], permissions: permissionDefinitions }),
      getInvitations: vi.fn().mockResolvedValue({ invitations: [] }),
      createInvitation: vi.fn(),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<InvitationsPage api={api} canManage actorSuperAdmin />)

    await screen.findByRole('heading', { name: 'Invitation management' })
    await user.click(screen.getByRole('button', { name: 'Invite user' }))
    const name = screen.getByLabelText('Name')
    const email = screen.getByLabelText('Email')
    await user.type(name, 'Lin')
    await user.type(email, 'lin@example.com')
    await user.click(screen.getByRole('button', { name: 'Send invitation' }))
    expect(await screen.findByText('Select at least one role.')).toBeVisible()

    await i18n.changeLanguage('zh-CN')

    expect(await screen.findByText('至少选择一个角色。')).toBeVisible()
    expect(screen.getByDisplayValue('Lin')).toBe(name)
    expect(screen.getByDisplayValue('lin@example.com')).toBe(email)
  })

  it('distinguishes forbidden failures and does not offer a misleading retry', () => {
    render(<AccessError error={problem('/problems/forbidden', 403, 'forbidden')} />)

    expect(screen.getByRole('heading', { name: 'Access denied' })).toBeVisible()
    expect(screen.getByText('Your account does not have permission to view this page.')).toBeVisible()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('announces route-level access denial without a retry prompt', async () => {
    const view = render(<AccessDenied />)

    expect(screen.getByRole('heading', { name: 'Access denied' })).toBeVisible()
    expect(screen.getByText('Your account does not have permission to view this page.')).toBeVisible()
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Access denied', { description: 'Your account does not have permission to view this page.' }))

    view.unmount()
    render(<AccessDenied />)
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(2))
  })

  it('distinguishes conflicts without offering a recovery action', () => {
    render(<AccessError error={problem('/problems/role-in-use', 409, 'role_in_use')} />)

    expect(screen.getByRole('heading', { name: 'This record changed' })).toBeVisible()
    expect(screen.getByText('This role is still assigned. Reassign users and invitations first.')).toBeVisible()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('distinguishes validation failures and dependency failures', async () => {
    const view = render(<AccessError error={problem('/problems/validation-failed', 422, 'validation_failed')} />)
    expect(screen.getByRole('heading', { name: 'Review the access details' })).toBeVisible()
    expect(screen.getByText('Review the highlighted fields and try again.')).toBeVisible()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()

    view.unmount()
    render(<AccessError error={new ApiTransportError('network')} />)
    expect(screen.getByRole('heading', { name: 'Access data is unavailable' })).toBeVisible()
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })

  it('keeps a stale role draft and requires a browser refresh', async () => {
    const replaceRole = vi.fn().mockRejectedValue(problem('/problems/stale-revision', 409, 'stale_revision'))
    const api = mockApi({ getRoles: vi.fn().mockResolvedValue({ roles: [systemRole, usersRole], permissions: permissionDefinitions }), replaceRole })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage />)

    await screen.findByRole('heading', { name: 'Role management' })
    const roleRow = screen.getByText('Users reader').closest('tr')
    expect(roleRow).not.toBeNull()
    await user.click(within(roleRow!).getByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }))
    await user.click(screen.getByRole('button', { name: 'Save role' }))
    await waitFor(() => expect(replaceRole).toHaveBeenCalledTimes(1))
    expect(screen.getByDisplayValue('Users reader')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Save role' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Discard draft and reload' })).not.toBeInTheDocument()
  })

  it('keeps a stale assignment draft and requires a browser refresh', async () => {
    const userRecord = {
      id: '019535d9-3df7-79fb-b466-fa907fa17f91',
      name: 'Ada',
      email: 'ada@example.com',
      createdAt: '2026-09-02T00:00:00Z',
      authVersion: 1,
      roles: [usersRole],
    }
    const getUsers = vi.fn().mockResolvedValue({ users: [userRecord] })
    const replaceUserRoles = vi.fn().mockRejectedValue(problem('/problems/stale-revision', 409, 'stale_revision'))
    const api = mockApi({
      getUsers,
      getRoles: vi.fn().mockResolvedValue({ roles: [usersRole, rolesRole], permissions: permissionDefinitions }),
      getInvitations: vi.fn().mockResolvedValue({ invitations: [] }),
      replaceUserRoles,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<UsersPage api={api} canManage />)

    await screen.findByText('Ada')
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await user.click(screen.getByRole('menuitem', { name: 'Assign roles' }))
    const dialog = await screen.findByRole('dialog', { name: 'Assign roles' })
    const usersCheckbox = within(dialog).getByRole('checkbox', { name: 'Users reader' })
    const rolesCheckbox = within(dialog).getByRole('checkbox', { name: 'Roles reader' })
    await user.click(usersCheckbox)
    await user.click(rolesCheckbox)
    await user.click(within(dialog).getByRole('button', { name: 'Save assignments' }))
    await waitFor(() => expect(replaceUserRoles).toHaveBeenCalledTimes(1))
    expect(within(dialog).getByRole('checkbox', { name: 'Users reader' })).not.toBeChecked()
    expect(within(dialog).getByRole('checkbox', { name: 'Roles reader' })).toBeChecked()
    expect(within(dialog).getByRole('button', { name: 'Save assignments' })).toBeDisabled()
    expect(screen.queryByRole('button', { name: 'Discard draft and reload' })).not.toBeInTheDocument()
  })

  it('keeps a role draft when its dialog is closed and reopened', async () => {
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole], permissions: permissionDefinitions }),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage />)

    await screen.findByRole('heading', { name: 'Role management' })
    await user.click(screen.getByRole('button', { name: 'Create role' }))
    await user.type(screen.getByLabelText('Role name'), 'Auditor')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Create role' }))
    expect(screen.getByDisplayValue('Auditor')).toBeVisible()
  })

  it('does not let a late role result close a newer editor', async () => {
    let resolveSave: ((role: Role) => void) | undefined
    const api = mockApi({
      getRoles: vi.fn().mockResolvedValue({ roles: [systemRole, usersRole], permissions: permissionDefinitions }),
      replaceRole: vi.fn(() => new Promise<Role>((resolve) => { resolveSave = resolve })),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<RolesPage api={api} canManage />)

    await screen.findByRole('heading', { name: 'Role management' })
    const roleRow = screen.getByText('Users reader').closest('tr')
    expect(roleRow).not.toBeNull()
    await user.click(within(roleRow!).getByRole('button', { name: 'More actions' }))
    await user.click(await screen.findByRole('menuitem', { name: 'Edit' }))
    await user.click(screen.getByRole('button', { name: 'Save role' }))
    await waitFor(() => expect(api.replaceRole).toHaveBeenCalledOnce())
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Create role' }))
    await user.type(screen.getByLabelText('Role name'), 'Auditor')

    resolveSave?.({ ...usersRole, name: 'Users updated', revision: 2 })
    const dialog = await screen.findByRole('dialog', { name: 'Create role' })
    expect(within(dialog).getByDisplayValue('Auditor')).toBeVisible()
    expect(dialog).toBeVisible()
  })

  it('keeps an invitation draft independent from interface language', async () => {
    const api = mockApi({
      getUsers: vi.fn().mockResolvedValue({ users: [{ id: '019535d9-3df7-79fb-b466-fa907fa17f91', name: 'Ada', email: 'ada@example.com', createdAt: '2026-09-02T00:00:00Z', authVersion: 1, roles: [usersRole] }] }),
      getRoles: vi.fn().mockResolvedValue({ roles: [usersRole], permissions: permissionDefinitions }),
      getInvitations: vi.fn().mockResolvedValue({ invitations: [] }),
    })
    const user = userEvent.setup()
    renderWithQueryClient(<InvitationsPage api={api} canManage actorSuperAdmin />)
    await screen.findByRole('heading', { name: 'Invitation management' })
    await user.click(screen.getByRole('button', { name: 'Invite user' }))
    await user.type(screen.getByLabelText('Name'), 'Lin')
    await user.click(screen.getByRole('button', { name: 'Cancel' }))
    await user.click(screen.getByRole('button', { name: 'Invite user' }))
    expect(screen.getByDisplayValue('Lin')).toBeVisible()
    await i18n.changeLanguage('zh-CN')
    expect(screen.getByDisplayValue('Lin')).toBeVisible()
  })

  it('opens user role assignment in a dialog and submits the optimistic version', async () => {
    const userRecord = { id: '019535d9-3df7-79fb-b466-fa907fa17f91', name: 'Ada', email: 'ada@example.com', createdAt: '2026-09-02T00:00:00Z', authVersion: 3, roles: [usersRole] }
    const replaceUserRoles = vi.fn().mockResolvedValue({ user: { ...userRecord, roles: [rolesRole], authVersion: 4 } })
    const api = mockApi({
      getUsers: vi.fn().mockResolvedValue({ users: [userRecord] }),
      getRoles: vi.fn().mockResolvedValue({ roles: [usersRole, rolesRole], permissions: permissionDefinitions }),
      getInvitations: vi.fn().mockResolvedValue({ invitations: [] }),
      replaceUserRoles,
    })
    const user = userEvent.setup()
    renderWithQueryClient(<UsersPage api={api} canManage />)
    await screen.findByText('Ada')
    await user.click(screen.getByRole('button', { name: 'More actions' }))
    await user.click(screen.getByRole('menuitem', { name: 'Assign roles' }))
    const dialog = screen.getByRole('dialog', { name: 'Assign roles' })
    await user.click(within(dialog).getByRole('checkbox', { name: 'Roles reader' }))
    await user.click(within(dialog).getByRole('checkbox', { name: 'Users reader' }))
    await user.click(within(dialog).getByRole('button', { name: 'Save assignments' }))
    await waitFor(() => expect(replaceUserRoles).toHaveBeenCalledWith(userRecord.id, { roleIds: [rolesRole.id], authVersion: 3 }))
  })
})
