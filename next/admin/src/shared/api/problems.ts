import { ApiProblemError, ApiProtocolError, ApiTransportError } from './client'
import type { FieldProblem } from './contracts'

const typeKeys: Record<string, string> = {
  '/problems/invalid-request': 'problems:invalidRequest',
  '/problems/invalid-credentials': 'problems:invalidCredentials',
  '/problems/unauthenticated': 'problems:unauthenticated',
  '/problems/forbidden': 'problems:forbidden',
  '/problems/invalid-setup-token': 'problems:invalidSetupToken',
  '/problems/invalid-password-reset-token': 'problems:invalidPasswordResetToken',
  '/problems/invalid-invitation': 'problems:invalidInvitation',
  '/problems/role-in-use': 'problems:roleInUse',
  '/problems/role-immutable': 'problems:roleImmutable',
  '/problems/last-super-admin': 'problems:lastSuperAdmin',
  '/problems/stale-revision': 'problems:staleRevision',
  '/problems/role-already-exists': 'problems:roleAlreadyExists',
  '/problems/invitation-pending': 'problems:invitationPending',
  '/problems/not-found': 'problems:notFound',
  '/problems/method-not-allowed': 'problems:methodNotAllowed',
  '/problems/setup-complete': 'problems:setupComplete',
  '/problems/content-too-large': 'problems:contentTooLarge',
  '/problems/unsupported-media-type': 'problems:unsupportedMediaType',
  '/problems/validation-failed': 'problems:validationFailed',
  '/problems/rate-limited': 'problems:rateLimited',
  '/problems/internal-error': 'problems:internalError',
  '/problems/service-unavailable': 'problems:serviceUnavailable',
  '/problems/mail-not-configured': 'problems:mailNotConfigured',
  '/problems/mail-task-not-found': 'problems:mailTaskNotFound',
  '/problems/mail-task-sending': 'problems:mailTaskSending',
  '/problems/mail-task-not-retryable': 'problems:mailTaskNotRetryable',
  '/problems/mail-settings-not-saved': 'problems:mailSettingsNotSaved',
  '/problems/permission-scope': 'problems:permissionScope',
}

const codeKeys: Record<string, string> = {
  account_disabled: 'problems:accountDisabled',
  self_user_operation: 'problems:selfUserOperation',
  invalid_request: 'problems:invalidRequest',
  invalid_credentials: 'problems:invalidCredentials',
  unauthenticated: 'problems:unauthenticated',
  forbidden: 'problems:forbidden',
  invalid_setup_token: 'problems:invalidSetupToken',
  invalid_password_reset_token: 'problems:invalidPasswordResetToken',
  invalid_invitation: 'problems:invalidInvitation',
  role_in_use: 'problems:roleInUse',
  role_immutable: 'problems:roleImmutable',
  last_super_admin: 'problems:lastSuperAdmin',
  stale_revision: 'problems:staleRevision',
  role_already_exists: 'problems:roleAlreadyExists',
  invitation_pending: 'problems:invitationPending',
  setup_complete: 'problems:setupComplete',
  validation_failed: 'problems:validationFailed',
  rate_limited: 'problems:rateLimited',
  service_unavailable: 'problems:serviceUnavailable',
  mail_not_configured: 'problems:mailNotConfigured',
  mail_task_sending: 'problems:mailTaskSending',
  mail_task_not_retryable: 'problems:mailTaskNotRetryable',
  mail_settings_not_saved: 'problems:mailSettingsNotSaved',
  permission_scope_forbidden: 'problems:permissionScope',
  invalid_mail_settings: 'problems:invalidMailSettings',
  invalid_email_change: 'problems:emailChangeInvalid',
  email_change_resend_too_soon: 'problems:emailChangeResendTooSoon',
  invalid_email_change_code: 'problems:emailChangeCodeInvalid',
  email_change_attempts_exceeded: 'problems:emailChangeAttemptsExceeded',
}

const fieldKeys: Record<string, string> = {
  required: 'problems:fields.required',
  invalid_name: 'problems:fields.invalidName',
  invalid_email: 'problems:fields.invalidEmail',
  invalid_password: 'problems:fields.invalidPassword',
  invalid_login_password: 'problems:fields.invalidLoginPassword',
  login_password_required: 'problems:fields.loginPasswordRequired',
  login_password_too_long: 'problems:fields.loginPasswordTooLong',
  invalid_value: 'problems:fields.invalidValue',
  email_already_registered: 'problems:fields.emailAlreadyRegistered',
  password_mismatch: 'problems:fields.passwordMismatch',
  empty_permissions: 'problems:fields.emptyPermissions',
  unknown_permission: 'problems:fields.unknownPermission',
  invalid_role: 'problems:fields.invalidRole',
  duplicate_role: 'problems:fields.duplicateRole',
  invalid_revision: 'problems:fields.invalidRevision',
  invalid_limit: 'problems:fields.invalidLimit',
  invalid_cursor: 'problems:fields.invalidCursor',
  invalid_role_set: 'problems:fields.invalidRoleSet',
  invalid_description: 'problems:fields.invalidDescription',
  invalid_retry_count: 'problems:fields.invalidRetryCount',
  invalid_retention_days: 'problems:fields.invalidRetentionDays',
  invalid_time: 'problems:fields.invalidTime',
  invalid_time_range: 'problems:fields.invalidTimeRange',
  invalid_batch: 'problems:fields.invalidBatch',
  invalid_id: 'problems:fields.invalidID',
  duplicate_id: 'problems:fields.duplicateID',
  invalid_locale: 'problems:fields.invalidValue',
  too_long: 'problems:fields.tooLong',
  control_character: 'problems:fields.invalidValue',
  invalid_icon: 'problems:fields.invalidIcon',
  unsupported_type: 'problems:fields.unsupportedImageType',
  same_email: 'problems:fields.sameEmail',
  invalid_code: 'problems:fields.invalidCode',
}

export function fieldMessageKey(code: string | undefined): string {
  return fieldKeys[code ?? ''] ?? 'problems:fields.invalidValue'
}

export function isUnauthenticated(error: unknown): boolean {
  return error instanceof ApiProblemError && error.problem.status === 401 && (
    error.problem.type === '/problems/unauthenticated' || error.problem.code === 'unauthenticated'
  )
}

export function isAccountDisabled(error: unknown): boolean {
  return error instanceof ApiProblemError && error.problem.status === 401 && error.problem.code === 'account_disabled'
}

export function isForbidden(error: unknown): boolean {
  return error instanceof ApiProblemError && (error.problem.status === 403 || error.problem.code === 'forbidden')
}

export function isRequestCancelled(error: unknown): boolean {
  return error instanceof ApiTransportError && error.aborted
}

export function isInvalidSetupToken(error: unknown): boolean {
  return error instanceof ApiProblemError && error.problem.status === 403 && (
    error.problem.type === '/problems/invalid-setup-token' || error.problem.code === 'invalid_setup_token'
  )
}

export function isSetupComplete(error: unknown): boolean {
  return error instanceof ApiProblemError && error.problem.status === 409 && (
    error.problem.type === '/problems/setup-complete' || error.problem.code === 'setup_complete'
  )
}

export function isInvalidPasswordResetToken(error: unknown): boolean {
  return error instanceof ApiProblemError && error.problem.status === 403 && (
    error.problem.type === '/problems/invalid-password-reset-token' || error.problem.code === 'invalid_password_reset_token'
  )
}

export function problemMessageKey(error: unknown): string {
  if (error instanceof ApiProblemError) {
    // Some personal-security failures share the validation or rate-limit
    // envelope but still need an actionable message. Preserve the existing
    // type-first behavior for all other problem codes.
    const code = error.problem.code ?? ''
    if (code === 'invalid_mail_settings' || code === 'invalid_email_change' || code === 'email_change_resend_too_soon' || code === 'invalid_email_change_code' || code === 'email_change_attempts_exceeded') return codeKeys[code]
    return typeKeys[error.problem.type] ?? codeKeys[code] ?? 'problems:generic'
  }
  if (error instanceof ApiTransportError) {
    return error.aborted ? 'problems:requestCancelled' : 'problems:network'
  }
  if (error instanceof ApiProtocolError) {
    return 'problems:malformedResponse'
  }
  return 'problems:generic'
}

export function translateProblem(error: unknown, t: unknown): string {
  return (t as (key: string) => string)(problemMessageKey(error))
}

export function translateProblemWithFields(error: unknown, t: unknown): string {
  const message = translateProblem(error, t)
  if (!(error instanceof ApiProblemError) || !error.problem.errors?.length) return message
  const fields = error.problem.errors
    .map((field) => translateFieldProblem(field, t))
    .filter((value): value is string => Boolean(value))
  return fields.length > 0 ? `${message} ${fields.join(' ')}` : message
}

export function translatePasswordResetProblem(error: unknown, t: unknown): string {
  return translateRateLimitedProblem(error, t, 'passwordResetRateLimited')
}

export function translateRateLimitedProblem(error: unknown, t: unknown, key: string): string {
  if (error instanceof ApiProblemError && error.problem.status === 429 && (
    error.problem.type === '/problems/rate-limited' || error.problem.code === 'rate_limited'
  )) {
    return (t as (value: string) => string)(`problems:${key}`)
  }
  return translateProblem(error, t)
}

export function translateRateLimitedProblemWithFields(error: unknown, t: unknown, key: string): string {
  if (error instanceof ApiProblemError && error.problem.status === 429 && (
    error.problem.type === '/problems/rate-limited' || error.problem.code === 'rate_limited'
  )) {
    return (t as (value: string) => string)(`problems:${key}`)
  }
  return translateProblemWithFields(error, t)
}

export function problemFieldKey(field: FieldProblem): string {
  return fieldMessageKey(field.code)
}

export function fieldProblemFor(error: unknown, pointer: string): FieldProblem | undefined {
  if (!(error instanceof ApiProblemError)) return undefined
  return error.problem.errors?.find((field) => field.pointer === pointer)
}

export function translateFieldProblem(field: FieldProblem | undefined, t: unknown): string | undefined {
  return field ? (t as (key: string) => string)(problemFieldKey(field)) : undefined
}

export function translateClientFieldError(
  error: { type?: string; message?: string } | undefined,
  t: unknown,
): string | undefined {
  if (!error) return undefined
  return (t as (key: string) => string)(fieldMessageKey(error.message))
}
