import {
  onlineUsersResponseSchema,
  sessionStatusSchema,
  type OnlineUser,
	authEnvelopeSchema,
	loginInputSchema,
	assignmentInputSchema,
	invitationAcceptanceInputSchema,
	invitationInputSchema,
	invitationsResponseSchema,
	invitationResponseSchema,
	passwordResetAcceptedSchema,
	passwordResetCompleteInputSchema,
	passwordResetRequestInputSchema,
	emailSettingsResponseSchema,
  emailTasksResponseSchema,
  emailTaskResponseSchema,
  emailTaskBulkResponseSchema,
  submittedEmailTaskResponseSchema,
	operationalWarningsSchema,
	operationLogRetentionSchema,
	operationLogResponseSchema,
	operationLogsResponseSchema,
	operationLogStatusSchema,
	systemIdentityResponseSchema,
  personalProfileResponseSchema,
  emailChangeResponseSchema,
  personalEmailChangeStatusSchema,
  personalPasswordInputSchema,
  personalEmailChangeInputSchema,
  personalEmailVerifyInputSchema,
  userEnvelopeSchema,
	problemDetailsSchema,
	roleMutationInputSchema,
	roleResponseSchema,
	rolesResponseSchema,
	roleOptionsResponseSchema,
	setupInputSchema,
  setupResponseSchema,
  setupStatusSchema,
	userRoleResponseSchema,
	usersResponseSchema,
  type ProblemDetails,
  type SetupStatus,
  type User,
  type Role,
  type Permission,
  type Invitation,
	  type RoleOption,
  type EmailSettings,
  type EmailTask,
  type OperationLog,
  type SystemIdentity,
  type EmailChange,
  type AccessUser,
} from './contracts'

export class ApiProblemError extends Error {
  readonly problem: ProblemDetails

  constructor(problem: ProblemDetails) {
    super(problem.title)
    this.name = 'ApiProblemError'
    this.problem = problem
  }
}

export class ApiProtocolError extends Error {
  constructor(message = 'The server returned an unexpected response.') {
    super(message)
    this.name = 'ApiProtocolError'
  }
}

export class ApiTransportError extends Error {
  readonly aborted: boolean

  constructor(message: string, options: { aborted?: boolean; cause?: unknown } = {}) {
    super(message, { cause: options.cause })
    this.name = 'ApiTransportError'
    this.aborted = options.aborted ?? false
  }
}

export interface ApiClient {
  getOnlineUsers?(signal?: AbortSignal): Promise<{ users: OnlineUser[] }>
  kickUser?(id: string, signal?: AbortSignal): Promise<void>
  getSetupStatus(signal?: AbortSignal): Promise<SetupStatus>
  setup(input: { token: string; name: string; email: string; password: string; locale?: 'en' | 'zh-CN' }, signal?: AbortSignal): Promise<void>
  login(input: { email: string; password: string; locale?: 'en' | 'zh-CN' }, signal?: AbortSignal): Promise<User>
  me(signal?: AbortSignal): Promise<User>
  getPersonalProfile?(signal?: AbortSignal): Promise<{ user: User; emailChange?: EmailChange | null }>
  updatePersonalName?(name: string, signal?: AbortSignal): Promise<User>
  updatePersonalLocale?(locale: 'en' | 'zh-CN', signal?: AbortSignal): Promise<User>
  savePersonalAvatar?(file: Blob, signal?: AbortSignal): Promise<User>
  removePersonalAvatar?(signal?: AbortSignal): Promise<User>
  changePersonalPassword?(input: { currentPassword: string; newPassword: string; confirmPassword: string }, signal?: AbortSignal): Promise<void>
  requestPersonalEmailChange?(input: { currentPassword: string; newEmail: string }, signal?: AbortSignal): Promise<EmailChange>
  getPersonalEmailChange?(signal?: AbortSignal): Promise<EmailChange | null>
  resendPersonalEmailChange?(signal?: AbortSignal): Promise<EmailChange>
  verifyPersonalEmailChange?(input: { requestId: string; code: string }, signal?: AbortSignal): Promise<void>
  checkSession?(signal?: AbortSignal): Promise<void>
	getRoles?(signal?: AbortSignal): Promise<{ roles: Role[]; permissions: Permission[]; combinations?: Array<{ key: string; labelKey: string; description: string; permissions: string[]; trigger?: string[] }> }>
	getRoleOptions?(signal?: AbortSignal): Promise<{ roles: RoleOption[] }>
	getRole?(id: string, signal?: AbortSignal): Promise<Role>
	createRole?(input: { name: string; description: string; permissions: string[] }, signal?: AbortSignal): Promise<Role>
	replaceRole?(id: string, input: { name: string; description: string; permissions: string[]; revision: number }, signal?: AbortSignal): Promise<Role>
	deleteRole?(id: string, signal?: AbortSignal): Promise<void>
	getUsers?(options?: { cursor?: string; limit?: number; q?: string; roleId?: string; status?: 'active' | 'disabled'; sort?: string; direction?: 'asc' | 'desc' }, signal?: AbortSignal): Promise<{ users: AccessUser[]; nextCursor?: string }>
	replaceUserRoles?(id: string, input: { roleIds: string[]; authVersion: number }, signal?: AbortSignal): Promise<{ user: AccessUser }>
	deactivateUser?(id: string, authVersion: number, signal?: AbortSignal): Promise<void>
	reactivateUser?(id: string, authVersion: number, signal?: AbortSignal): Promise<void>
	deleteUser?(id: string, authVersion: number, signal?: AbortSignal): Promise<void>
	getInvitations?(options?: { cursor?: string; limit?: number; q?: string; roleId?: string; status?: 'pending' | 'expired'; sort?: string; direction?: 'asc' | 'desc' }, signal?: AbortSignal): Promise<{ invitations: Invitation[]; nextCursor?: string }>
	createInvitation?(input: { name: string; email: string; roleIds: string[] }, signal?: AbortSignal): Promise<{ invitation: Invitation }>
	resendInvitation?(id: string, signal?: AbortSignal): Promise<{ invitation: Invitation }>
	revokeInvitation?(id: string, signal?: AbortSignal): Promise<void>
	acceptInvitation?(input: { token: string; password: string }, signal?: AbortSignal): Promise<void>
	logout(signal?: AbortSignal): Promise<void>
	requestPasswordReset(input: { email: string }, signal?: AbortSignal): Promise<void>
	completePasswordReset(input: { token: string; password: string }, signal?: AbortSignal): Promise<void>
	getEmailSettings?(signal?: AbortSignal): Promise<EmailSettings>
	saveEmailSettings?(input: { host: string; port: number; security: 'none' | 'starttls' | 'tls'; username: string; password?: string; clearPassword?: boolean; fromAddress: string; fromName: string; defaultLocale: 'en' | 'zh-CN'; autoRetryCount?: number; retentionDays?: number; revision: number }, signal?: AbortSignal): Promise<EmailSettings>
	testEmailSettings?(input: { recipient: string }, signal?: AbortSignal): Promise<{ status: 'accepted'; task?: EmailTask } | void>
  getEmailTasks?(options?: { cursor?: string; limit?: number; recipient?: string; kind?: string; purpose?: string; status?: 'queued' | 'sending' | 'waiting_retry' | 'sent' | 'failed'; from?: string; to?: string; failedOnly?: boolean }, signal?: AbortSignal): Promise<{ tasks: EmailTask[]; nextCursor?: string }>
  getEmailTask?(id: string, signal?: AbortSignal): Promise<EmailTask>
  retryEmailTask?(id: string, signal?: AbortSignal): Promise<EmailTask>
  deleteEmailTask?(id: string, signal?: AbortSignal): Promise<void>
  retryEmailTasks?(ids: string[], signal?: AbortSignal): Promise<{ succeeded: number; skipped: number; failed: number; items: Array<{ id: string; result: 'succeeded' | 'skipped' | 'failed'; code?: string }> }>
  deleteEmailTasks?(ids: string[], signal?: AbortSignal): Promise<{ succeeded: number; skipped: number; failed: number; items: Array<{ id: string; result: 'succeeded' | 'skipped' | 'failed'; code?: string }> }>
  getSubmittedTestEmailStatus?(id: string, signal?: AbortSignal): Promise<EmailTask>
	getOperationalWarnings?(signal?: AbortSignal): Promise<{ warnings: Array<{ key: string; severity: string }> }>
	getOperationLogStatus?(signal?: AbortSignal): Promise<{ state: 'unknown' | 'healthy' | 'failed' | 'recovered'; failureCount: number; lastFailureAt?: string; lastSuccessAt?: string }>
	getOperationLogs?(options?: { cursor?: string; limit?: number; from?: string; to?: string; actorId?: string; action?: string; objectType?: string; objectId?: string; result?: 'success' | 'failure' }, signal?: AbortSignal): Promise<{ logs: OperationLog[]; nextCursor?: string }>
	getOperationLog?(id: string, signal?: AbortSignal): Promise<OperationLog>
	getOperationLogRetention?(signal?: AbortSignal): Promise<{ retentionDays: number; revision: number; updatedAt?: string }>
	saveOperationLogRetention?(input: { retentionDays: number; revision: number }, signal?: AbortSignal): Promise<{ retentionDays: number; revision: number; updatedAt?: string }>
	getPublicSystemIdentity?(signal?: AbortSignal): Promise<SystemIdentity>
	getSystemIdentity?(signal?: AbortSignal): Promise<SystemIdentity>
	saveSystemIdentity?(input: FormData, signal?: AbortSignal): Promise<SystemIdentity>
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
  expectedStatus: number
}

const JSON_HEADERS = {
  Accept: 'application/json, application/problem+json',
  'Cache-Control': 'no-store',
}

async function request<T>(path: string, schema: { parse(value: unknown): T }, options: RequestOptions): Promise<T> {
  const headers = new Headers(JSON_HEADERS)
  if (options.body !== undefined) {
	    if (typeof FormData === 'undefined' || !(options.body instanceof FormData)) headers.set('Content-Type', 'application/json')
  }

  let response: Response
  try {
    response = await fetch(path, {
      method: options.method ?? 'GET',
      credentials: 'same-origin',
      cache: 'no-store',
      headers,
      body: options.body === undefined ? undefined : (typeof FormData !== 'undefined' && options.body instanceof FormData ? options.body : JSON.stringify(options.body)),
      signal: options.signal,
    })
  } catch (error) {
    const aborted = (error instanceof DOMException || error instanceof Error) && error.name === 'AbortError'
    throw new ApiTransportError(aborted ? 'The request was cancelled.' : 'The network request failed.', {
      aborted,
      cause: error,
    })
  }

  if (!response.ok) {
    const contentType = response.headers.get('content-type')?.split(';', 1)[0]?.trim().toLowerCase()
    if (contentType !== 'application/problem+json') {
      throw new ApiProtocolError()
    }
    let body: unknown
    try {
      body = await response.json()
    } catch {
      throw new ApiProtocolError('The server returned malformed error data.')
    }
    const problem = problemDetailsSchema.safeParse(body)
    if (!problem.success || problem.data.status !== response.status) {
      throw new ApiProtocolError()
    }
    throw new ApiProblemError(problem.data)
  }

  if (response.status !== options.expectedStatus) {
    throw new ApiProtocolError()
  }

  if (response.status === 204) {
    return undefined as T
  }
  const contentType = response.headers.get('content-type')?.split(';', 1)[0]?.trim().toLowerCase()
  if (contentType !== 'application/json') {
    throw new ApiProtocolError()
  }
  let body: unknown
  try {
    body = await response.json()
  } catch {
    throw new ApiProtocolError('The server returned malformed data.')
  }
  try {
    return schema.parse(body)
  } catch {
    throw new ApiProtocolError('The server returned data in an unexpected shape.')
  }
}

export function createApiClient(): ApiClient {
  return {
    getOnlineUsers: (signal) => request('/api/online-users', onlineUsersResponseSchema, { signal, expectedStatus: 200 }),
    kickUser: async (id, signal) => { await request(`/api/online-users/${encodeURIComponent(id)}/kick`, { parse: (value: unknown) => value as undefined }, { method: 'POST', signal, expectedStatus: 204 }) },
    checkSession: async (signal) => { await request('/api/auth/session-status', sessionStatusSchema, { signal, expectedStatus: 200 }) },
    getSetupStatus: (signal) => request('/api/setup/status', setupStatusSchema, { signal, expectedStatus: 200 }),
    setup: async (input, signal) => {
      const body = setupInputSchema.parse(input)
      await request('/api/setup', setupResponseSchema, { method: 'POST', body, signal, expectedStatus: 201 })
    },
    login: async (input, signal) => {
		const body = loginInputSchema.parse(input)
		const result = await request('/api/auth/login', authEnvelopeSchema, { method: 'POST', body, signal, expectedStatus: 200 })
		return 'roles' in result ? { ...result.user, roles: result.roles, permissions: result.permissions, superAdmin: result.superAdmin } : result.user
	},
		me: async (signal) => {
			const result = await request('/api/auth/me', authEnvelopeSchema, { signal, expectedStatus: 200 })
			return 'roles' in result ? { ...result.user, roles: result.roles, permissions: result.permissions, superAdmin: result.superAdmin } : result.user
    },
    getPersonalProfile: async (signal) => request('/api/auth/me/profile', personalProfileResponseSchema, { signal, expectedStatus: 200 }),
    updatePersonalName: async (name, signal) => (await request('/api/auth/me/profile', userEnvelopeSchema, { method: 'PUT', body: { name }, signal, expectedStatus: 200 })).user,
    updatePersonalLocale: async (locale, signal) => (await request('/api/auth/me/preferences', userEnvelopeSchema, { method: 'PUT', body: { locale }, signal, expectedStatus: 200 })).user,
    savePersonalAvatar: async (file, signal) => { const body = new FormData(); body.append('avatar', file, 'avatar.png'); return (await request('/api/auth/me/avatar', userEnvelopeSchema, { method: 'PUT', body, signal, expectedStatus: 200 })).user },
    removePersonalAvatar: async (signal) => (await request('/api/auth/me/avatar', userEnvelopeSchema, { method: 'DELETE', signal, expectedStatus: 200 })).user,
    changePersonalPassword: async (input, signal) => { const body = personalPasswordInputSchema.parse(input); await request('/api/auth/me/password', { parse: (value: unknown) => value as undefined }, { method: 'PUT', body, signal, expectedStatus: 204 }) },
    requestPersonalEmailChange: async (input, signal) => (await request('/api/auth/me/email-change', emailChangeResponseSchema, { method: 'POST', body: personalEmailChangeInputSchema.parse(input), signal, expectedStatus: 202 })).emailChange,
    getPersonalEmailChange: async (signal) => (await request('/api/auth/me/email-change', personalEmailChangeStatusSchema, { signal, expectedStatus: 200 })).emailChange,
    resendPersonalEmailChange: async (signal) => (await request('/api/auth/me/email-change/resend', emailChangeResponseSchema, { method: 'POST', signal, expectedStatus: 202 })).emailChange,
    verifyPersonalEmailChange: async (input, signal) => { const body = personalEmailVerifyInputSchema.parse(input); await request('/api/auth/me/email-change/verify', { parse: (value: unknown) => value as undefined }, { method: 'POST', body, signal, expectedStatus: 204 }) },
		getRoles: async (signal) => request('/api/roles', rolesResponseSchema, { signal, expectedStatus: 200 }),
		getRoleOptions: async (signal) => request('/api/access/role-options', roleOptionsResponseSchema, { signal, expectedStatus: 200 }),
		getRole: async (id, signal) => (await request(`/api/roles/${encodeURIComponent(id)}`, roleResponseSchema, { signal, expectedStatus: 200 })).role,
		createRole: async (input, signal) => (await request('/api/roles', roleResponseSchema, { method: 'POST', body: roleMutationInputSchema.parse(input), signal, expectedStatus: 201 })).role,
		replaceRole: async (id, input, signal) => (await request(`/api/roles/${encodeURIComponent(id)}`, roleResponseSchema, { method: 'PUT', body: roleMutationInputSchema.parse(input), signal, expectedStatus: 200 })).role,
		deleteRole: async (id, signal) => { await request(`/api/roles/${encodeURIComponent(id)}`, { parse: (value: unknown) => value as undefined }, { method: 'DELETE', signal, expectedStatus: 204 }) },
		getUsers: async (options, signal) => {
			const query = new URLSearchParams(); if (options?.cursor) query.set('cursor', options.cursor); if (options?.limit !== undefined) query.set('limit', String(options.limit)); if (options?.q) query.set('q', options.q); if (options?.roleId) query.set('roleId', options.roleId); if (options?.status) query.set('status', options.status); if (options?.sort) query.set('sort', options.sort); if (options?.direction) query.set('direction', options.direction)
			return request(`/api/users${query.size ? `?${query.toString()}` : ''}`, usersResponseSchema, { signal, expectedStatus: 200 })
		},
		replaceUserRoles: async (id, input, signal) => request(`/api/users/${encodeURIComponent(id)}/roles`, userRoleResponseSchema, { method: 'PUT', body: assignmentInputSchema.parse(input), signal, expectedStatus: 200 }),
		deactivateUser: async (id, authVersion, signal) => { await request(`/api/users/${encodeURIComponent(id)}/deactivate`, { parse: () => undefined }, { method: 'POST', body: { authVersion }, signal, expectedStatus: 200 }) },
		reactivateUser: async (id, authVersion, signal) => { await request(`/api/users/${encodeURIComponent(id)}/reactivate`, { parse: () => undefined }, { method: 'POST', body: { authVersion }, signal, expectedStatus: 200 }) },
		deleteUser: async (id, authVersion, signal) => { await request(`/api/users/${encodeURIComponent(id)}`, { parse: () => undefined }, { method: 'DELETE', body: { authVersion }, signal, expectedStatus: 204 }) },
		getInvitations: async (options, signal) => {
			const query = new URLSearchParams(); if (options?.cursor) query.set('cursor', options.cursor); if (options?.limit !== undefined) query.set('limit', String(options.limit)); if (options?.q) query.set('q', options.q); if (options?.roleId) query.set('roleId', options.roleId); if (options?.status) query.set('status', options.status); if (options?.sort) query.set('sort', options.sort); if (options?.direction) query.set('direction', options.direction)
			return request(`/api/user-invitations${query.size ? `?${query.toString()}` : ''}`, invitationsResponseSchema, { signal, expectedStatus: 200 })
		},
		createInvitation: async (input, signal) => request('/api/user-invitations', invitationResponseSchema, { method: 'POST', body: invitationInputSchema.parse(input), signal, expectedStatus: 201 }),
		resendInvitation: async (id, signal) => request(`/api/user-invitations/${encodeURIComponent(id)}/resend`, invitationResponseSchema, { method: 'POST', signal, expectedStatus: 202 }),
		revokeInvitation: async (id, signal) => { await request(`/api/user-invitations/${encodeURIComponent(id)}`, { parse: (value: unknown) => value as undefined }, { method: 'DELETE', signal, expectedStatus: 204 }) },
		acceptInvitation: async (input, signal) => { await request('/api/auth/invitations/accept', { parse: (value: unknown) => value as undefined }, { method: 'POST', body: invitationAcceptanceInputSchema.parse(input), signal, expectedStatus: 204 }) },
    logout: async (signal) => {
      await request('/api/auth/logout', { parse: (value: unknown) => value as undefined }, { method: 'POST', signal, expectedStatus: 204 })
    },
    requestPasswordReset: async (input, signal) => {
		const body = passwordResetRequestInputSchema.parse(input)
      await request('/api/auth/password-reset/request', passwordResetAcceptedSchema, { method: 'POST', body, signal, expectedStatus: 202 })
    },
    completePasswordReset: async (input, signal) => {
		const body = passwordResetCompleteInputSchema.parse(input)
	      await request('/api/auth/password-reset/complete', { parse: (value: unknown) => value as undefined }, { method: 'POST', body, signal, expectedStatus: 204 })
	    },
		getEmailSettings: async (signal) => (await request('/api/settings/email', emailSettingsResponseSchema, { signal, expectedStatus: 200 })).email,
		saveEmailSettings: async (input, signal) => (await request('/api/settings/email', emailSettingsResponseSchema, { method: 'PUT', body: input, signal, expectedStatus: 200 })).email,
		testEmailSettings: async (input, signal) => request('/api/settings/email/test', submittedEmailTaskResponseSchema, { method: 'POST', body: input, signal, expectedStatus: 202 }),
    getEmailTasks: async (options, signal) => {
      const query = new URLSearchParams(); if (options?.cursor) query.set('cursor', options.cursor); if (options?.limit !== undefined) query.set('limit', String(options.limit)); if (options?.recipient) query.set('recipient', options.recipient); if (options?.kind) query.set('kind', options.kind); else if (options?.purpose) query.set('purpose', options.purpose); if (options?.status) query.set('status', options.status); if (options?.from) query.set('from', options.from); if (options?.to) query.set('to', options.to); if (options?.failedOnly) query.set('failedOnly', 'true')
      return request(`/api/mail-tasks${query.size ? `?${query.toString()}` : ''}`, emailTasksResponseSchema, { signal, expectedStatus: 200 })
    },
    getEmailTask: async (id, signal) => (await request(`/api/mail-tasks/${encodeURIComponent(id)}`, emailTaskResponseSchema, { signal, expectedStatus: 200 })).task,
    retryEmailTask: async (id, signal) => (await request(`/api/mail-tasks/${encodeURIComponent(id)}/retry`, emailTaskResponseSchema, { method: 'POST', signal, expectedStatus: 200 })).task,
    deleteEmailTask: async (id, signal) => { await request(`/api/mail-tasks/${encodeURIComponent(id)}`, { parse: (value: unknown) => value as undefined }, { method: 'DELETE', signal, expectedStatus: 204 }) },
    retryEmailTasks: async (ids, signal) => request('/api/mail-tasks/bulk-retry', emailTaskBulkResponseSchema, { method: 'POST', body: { ids }, signal, expectedStatus: 200 }),
    deleteEmailTasks: async (ids, signal) => request('/api/mail-tasks/bulk-delete', emailTaskBulkResponseSchema, { method: 'POST', body: { ids }, signal, expectedStatus: 200 }),
    getSubmittedTestEmailStatus: async (id, signal) => (await request(`/api/settings/email/test/${encodeURIComponent(id)}`, emailTaskResponseSchema, { signal, expectedStatus: 200 })).task,
		getOperationalWarnings: async (signal) => request('/api/operational-warnings', operationalWarningsSchema, { signal, expectedStatus: 200 }),
		getOperationLogStatus: async (signal) => request('/api/operation-logs/status', operationLogStatusSchema, { signal, expectedStatus: 200 }),
		getOperationLogs: async (options, signal) => {
			const query = new URLSearchParams(); if (options?.cursor) query.set('cursor', options.cursor); if (options?.limit !== undefined) query.set('limit', String(options.limit)); if (options?.from) query.set('from', options.from); if (options?.to) query.set('to', options.to); if (options?.actorId) query.set('actorId', options.actorId); if (options?.action) query.set('action', options.action); if (options?.objectType) query.set('objectType', options.objectType); if (options?.objectId) query.set('objectId', options.objectId); if (options?.result) query.set('result', options.result)
			return request(`/api/operation-logs${query.size ? `?${query.toString()}` : ''}`, operationLogsResponseSchema, { signal, expectedStatus: 200 })
		},
		getOperationLog: async (id, signal) => (await request(`/api/operation-logs/${encodeURIComponent(id)}`, operationLogResponseSchema, { signal, expectedStatus: 200 })).log,
		getOperationLogRetention: async (signal) => request('/api/settings/operation-log', operationLogRetentionSchema, { signal, expectedStatus: 200 }),
		saveOperationLogRetention: async (input, signal) => (await request('/api/settings/operation-log', operationLogRetentionSchema, { method: 'PUT', body: input, signal, expectedStatus: 200 })),
		getPublicSystemIdentity: (signal) => request('/api/public/system-identity', systemIdentityResponseSchema, { signal, expectedStatus: 200 }),
		getSystemIdentity: (signal) => request('/api/settings/system-identity', systemIdentityResponseSchema, { signal, expectedStatus: 200 }),
		saveSystemIdentity: async (input, signal) => request('/api/settings/system-identity', systemIdentityResponseSchema, { method: 'PUT', body: input, signal, expectedStatus: 200 }),
  }
}
