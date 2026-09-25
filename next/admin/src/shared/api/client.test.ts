import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'
import { ApiProblemError, ApiProtocolError, createApiClient } from './client'
import { server } from '@/test/msw'

const api = createApiClient()
const user = { id: '00000000-0000-4000-8000-000000000001', name: 'Admin', email: 'admin@example.com' }

describe('Fetch API boundary', () => {
  it('validates endpoint responses and sends same-origin JSON headers', async () => {
    let request: Request | undefined
    server.use(http.get('/api/setup/status', ({ request: incoming }) => {
      request = incoming
      return HttpResponse.json({ status: 'required' }, { headers: { 'Cache-Control': 'no-store' } })
    }))

    await expect(api.getSetupStatus()).resolves.toEqual({ status: 'required' })
    expect(request?.credentials).toBe('same-origin')
    expect(request?.headers.get('accept')).toContain('application/problem+json')
    expect(request?.headers.get('cache-control')).toBe('no-store')
  })

  it('parses RFC 9457 responses into a typed problem error', async () => {
    server.use(http.post('/api/auth/login', () => HttpResponse.json({
      type: '/problems/invalid-credentials',
      title: 'Invalid credentials',
      status: 401,
      code: 'invalid_credentials',
    }, { status: 401, headers: { 'Content-Type': 'application/problem+json' } })))

    try {
      await api.login({ email: user.email, password: 'wrong password 123' })
      throw new Error('expected invalid credentials')
    } catch (error) {
      expect(error).toBeInstanceOf(ApiProblemError)
      expect((error as ApiProblemError).problem.code).toBe('invalid_credentials')
      expect((error as ApiProblemError).problem.status).toBe(401)
    }
  })

  it('rejects a successful response with the wrong media type and accepts logout 204', async () => {
    server.use(
      http.get('/api/auth/me', () => new HttpResponse('{}', { headers: { 'Content-Type': 'text/plain' } })),
      http.post('/api/auth/logout', () => new HttpResponse(null, { status: 204 })),
    )
    await expect(api.me()).rejects.toBeInstanceOf(ApiProtocolError)
    await expect(api.logout()).resolves.toBeUndefined()
  })

	it('rejects a valid body returned with the wrong success status', async () => {
    server.use(http.get('/api/setup/status', () => HttpResponse.json({ status: 'required' }, { status: 201 })))
		await expect(api.getSetupStatus()).rejects.toBeInstanceOf(ApiProtocolError)
	})

	it('uses accepted and no-content contracts for password recovery', async () => {
		let requestBody: unknown
		server.use(
			http.post('/api/auth/password-reset/request', async ({ request: incoming }) => {
				requestBody = await incoming.json()
				return HttpResponse.json({ status: 'accepted' }, { status: 202 })
			}),
			http.post('/api/auth/password-reset/complete', ({ request: incoming }) => {
				expect(incoming.headers.get('content-type')).toBe('application/json')
				return new HttpResponse(null, { status: 204 })
			}),
		)

		await expect(api.requestPasswordReset({ email: 'ada@example.com' })).resolves.toBeUndefined()
		expect(requestBody).toEqual({ email: 'ada@example.com' })
		await expect(api.completePasswordReset({ token: `v1.${'A'.repeat(22)}.${'B'.repeat(43)}`, password: 'Aa1!xxxx' })).resolves.toBeUndefined()
	})

	it('includes the selected recipient when testing email settings', async () => {
		let requestBody: unknown
		server.use(http.post('/api/settings/email/test', async ({ request: incoming }) => {
			requestBody = await incoming.json()
			return HttpResponse.json({ status: 'accepted' }, { status: 202 })
		}))

		await expect(api.testEmailSettings?.({ recipient: 'real@example.com' })).resolves.toEqual({ status: 'accepted' })
		expect(requestBody).toEqual({ recipient: 'real@example.com' })
	})

	it('encodes access-list search, role, status, and sort options in the query string', async () => {
		let usersURL = ''
		let invitationsURL = ''
		server.use(
			http.get('/api/users', ({ request }) => {
				usersURL = request.url
				return HttpResponse.json({ users: [] })
			}),
			http.get('/api/user-invitations', ({ request }) => {
				invitationsURL = request.url
				return HttpResponse.json({ invitations: [] })
			}),
		)

		await expect(api.getUsers?.({ q: 'Ada_%', roleId: '00000000-0000-4000-8000-000000000002', sort: 'name', direction: 'asc' })).resolves.toEqual({ users: [] })
		await expect(api.getInvitations?.({ q: 'Lin', roleId: '00000000-0000-4000-8000-000000000002', status: 'expired', sort: 'expiresAt', direction: 'desc' })).resolves.toEqual({ invitations: [] })
		expect(new URL(usersURL).searchParams.get('q')).toBe('Ada_%')
		expect(new URL(usersURL).searchParams.get('roleId')).toBe('00000000-0000-4000-8000-000000000002')
		expect(new URL(usersURL).searchParams.get('sort')).toBe('name')
		expect(new URL(usersURL).searchParams.get('direction')).toBe('asc')
		expect(new URL(invitationsURL).searchParams.get('q')).toBe('Lin')
		expect(new URL(invitationsURL).searchParams.get('roleId')).toBe('00000000-0000-4000-8000-000000000002')
		expect(new URL(invitationsURL).searchParams.get('status')).toBe('expired')
		expect(new URL(invitationsURL).searchParams.get('sort')).toBe('expiresAt')
		expect(new URL(invitationsURL).searchParams.get('direction')).toBe('desc')
	})

	it('uses personal settings contracts and multipart avatar uploads', async () => {
		let avatarRequest: Request | undefined
		server.use(
			http.get('/api/auth/me/profile', () => HttpResponse.json({ user: { ...user, locale: 'en', hasAvatar: true, avatarUrl: '/api/users/' + user.id + '/avatar?v=2', avatarVersion: 2 }, emailChange: null })),
			http.put('/api/auth/me/avatar', ({ request }) => {
				avatarRequest = request
				return HttpResponse.json({ user: { ...user, hasAvatar: true, avatarUrl: '/api/users/' + user.id + '/avatar?v=3', avatarVersion: 3 } })
			}),
			http.post('/api/auth/me/email-change', () => HttpResponse.json({ emailChange: { id: '00000000-0000-4000-8000-000000000003', oldEmail: user.email, newEmail: 'new@example.com', expiresAt: '2026-01-01T00:00:00.000Z', resendAvailableAt: '2025-12-31T23:51:00.000Z', attemptsRemaining: 5, revision: 1 } }, { status: 202 })),
		)
		await expect(api.getPersonalProfile?.()).resolves.toMatchObject({ user: { hasAvatar: true }, emailChange: null })
		await expect(api.savePersonalAvatar?.(new Blob(['avatar'], { type: 'image/png' }))).resolves.toMatchObject({ hasAvatar: true, avatarVersion: 3 })
		expect(avatarRequest?.headers.get('content-type')).toMatch(/^multipart\/form-data; boundary=/)
		await expect(api.requestPersonalEmailChange?.({ currentPassword: 'Aa1!old', newEmail: 'new@example.com' })).resolves.toMatchObject({ newEmail: 'new@example.com' })
	})

	it('loads role filter options through the access endpoint', async () => {
		server.use(http.get('/api/access/role-options', () => HttpResponse.json({ roles: [{ id: '00000000-0000-4000-8000-000000000002', name: 'Users reader' }] })))
		await expect(api.getRoleOptions?.()).resolves.toEqual({ roles: [{ id: '00000000-0000-4000-8000-000000000002', name: 'Users reader' }] })
	})
})
