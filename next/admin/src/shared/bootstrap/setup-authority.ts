const TOKEN_PATTERN = /^[A-Za-z0-9_-]{43}$/
const PASSWORD_RESET_TOKEN_PATTERN = /^v1\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}$/
const INVITATION_TOKEN_PATTERN = PASSWORD_RESET_TOKEN_PATTERN

let setupAuthority: string | undefined
let passwordResetAuthority: string | undefined
let invitationAuthority: string | undefined

export function isCanonicalSetupAuthority(value: string): boolean {
	return TOKEN_PATTERN.test(value)
}

export function isCanonicalPasswordResetAuthority(value: string): boolean {
	return PASSWORD_RESET_TOKEN_PATTERN.test(value)
}

export function isCanonicalInvitationAuthority(value: string): boolean {
	return INVITATION_TOKEN_PATTERN.test(value)
}

function captureAuthority(
	location: Pick<Location, 'pathname' | 'search' | 'hash'>,
	history: Pick<History, 'state' | 'replaceState'>,
	pathname: string,
	validate: (value: string) => boolean,
): string | undefined {
	if (location.pathname !== pathname) return undefined

	const hasFragment = location.hash.length > 0
	let candidate: string | undefined
	if (hasFragment) {
		try {
			const fragment = location.hash.slice(1)
			const params = new URLSearchParams(fragment)
			const tokens = params.getAll('token')
			const entries = [...params.keys()]
			if (tokens.length === 1 && entries.length === 1 && validate(tokens[0])) {
				candidate = tokens[0]
			}
		} catch {
			candidate = undefined
		}
		history.replaceState(history.state, '', `${location.pathname}${location.search}`)
	}
	return candidate
}

export function captureSetupAuthority(
  location: Pick<Location, 'pathname' | 'search' | 'hash'> = window.location,
  history: Pick<History, 'state' | 'replaceState'> = window.history,
): string | undefined {
	setupAuthority = captureAuthority(location, history, '/setup', isCanonicalSetupAuthority)
	return setupAuthority
}

export function getSetupAuthority(): string | undefined {
  return setupAuthority
}

export function clearSetupAuthority(): void {
	setupAuthority = undefined
}

export function capturePasswordResetAuthority(
	location: Pick<Location, 'pathname' | 'search' | 'hash'> = window.location,
	history: Pick<History, 'state' | 'replaceState'> = window.history,
): string | undefined {
	passwordResetAuthority = captureAuthority(location, history, '/reset-password', isCanonicalPasswordResetAuthority)
	return passwordResetAuthority
}

export function getPasswordResetAuthority(): string | undefined {
	return passwordResetAuthority
}

export function clearPasswordResetAuthority(): void {
	passwordResetAuthority = undefined
}

export function captureInvitationAuthority(
	location: Pick<Location, 'pathname' | 'search' | 'hash'> = window.location,
	history: Pick<History, 'state' | 'replaceState'> = window.history,
): string | undefined {
	invitationAuthority = captureAuthority(location, history, '/accept-invitation', isCanonicalInvitationAuthority)
	return invitationAuthority
}

export function getInvitationAuthority(): string | undefined {
	return invitationAuthority
}

export function clearInvitationAuthority(): void {
	invitationAuthority = undefined
}
