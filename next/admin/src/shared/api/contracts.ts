import { z } from 'zod'

export const userSchema = z
  .object({
    id: z.string().uuid(),
    name: z.string(),
    email: z.string(),
    locale: z.enum(['en', 'zh-CN']).optional(),
    avatarUrl: z.string().optional(),
    hasAvatar: z.boolean().default(false),
    avatarVersion: z.number().int().nonnegative().optional(),
    roles: z.array(z.lazy(() => roleSchema)).optional(),
    permissions: z.array(z.string()).optional(),
    superAdmin: z.boolean().optional(),
  })
  .strict()

export type User = z.infer<typeof userSchema>

export const userEnvelopeSchema = z
  .object({ user: userSchema })
  .strict()

export const permissionSchema = z.object({
  key: z.string(),
  resource: z.string(),
  action: z.string(),
  labelKey: z.string(),
  description: z.string(),
  dependencies: z.array(z.string()).optional(),
}).strict()

export const roleOptionSchema = z.object({ id: z.string().uuid(), name: z.string() }).strict()

export const roleSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  description: z.string(),
  system: z.string().optional(),
  permissions: z.array(z.string()),
  revision: z.number().int().positive(),
  assignmentCount: z.number().int().nonnegative().optional(),
  createdAt: z.string().optional(),
  updatedAt: z.string().optional(),
}).strict()

export type Role = z.infer<typeof roleSchema>
export type Permission = z.infer<typeof permissionSchema>
export type PermissionCombination = { key: string; labelKey: string; description: string; permissions: string[]; trigger?: string[] }
export type RoleOption = z.infer<typeof roleOptionSchema>

export const principalEnvelopeSchema = z.object({
  user: z.object({ id: z.string().uuid(), name: z.string(), email: z.string(), locale: z.enum(['en', 'zh-CN']).optional(), avatarUrl: z.string().optional(), hasAvatar: z.boolean().default(false), avatarVersion: z.number().int().nonnegative().optional() }).strict(),
  roles: z.array(roleSchema),
  permissions: z.array(z.string()),
  superAdmin: z.boolean(),
}).strict()

export type Principal = z.infer<typeof principalEnvelopeSchema>
export const authEnvelopeSchema = z.union([userEnvelopeSchema, principalEnvelopeSchema])

export const rolesResponseSchema = z.object({
  roles: z.array(roleSchema),
  permissions: z.array(permissionSchema),
  combinations: z.array(z.object({ key: z.string(), labelKey: z.string(), description: z.string(), permissions: z.array(z.string()), trigger: z.array(z.string()).optional() }).strict()).optional(),
}).strict()

export const roleOptionsResponseSchema = z.object({ roles: z.array(roleOptionSchema) }).strict()

export const roleResponseSchema = z.object({ role: roleSchema }).strict()

export const accessUserSchema = z.object({
  id: z.string().uuid(),
  name: z.string(),
  email: z.string(),
  locale: z.enum(['en', 'zh-CN']).optional(),
  avatarUrl: z.string().optional(),
  hasAvatar: z.boolean().default(false),
  avatarVersion: z.number().int().nonnegative().optional(),
  createdAt: z.string(),
  authVersion: z.number().int().positive(),
  disabled: z.boolean().default(false),
  roles: z.array(roleSchema),
}).strict()

export const usersResponseSchema = z.object({ users: z.array(accessUserSchema), nextCursor: z.string().optional() }).strict()
export type AccessUser = z.infer<typeof accessUserSchema>
export const userRoleResponseSchema = z.object({ user: accessUserSchema }).strict()
export const invitationSchema = z.object({
  creator: z.object({ id: z.string().optional(), name: z.string().optional(), email: z.string().optional(), deleted: z.boolean().optional() }).optional(),
  id: z.string().uuid(),
  name: z.string(),
  email: z.string(),
  locale: z.enum(['en', 'zh-CN']),
  roles: z.array(roleSchema),
  expiresAt: z.string(),
  createdAt: z.string(),
  revision: z.number().int().positive(),
}).strict()
export type Invitation = z.infer<typeof invitationSchema>
export const invitationsResponseSchema = z.object({ invitations: z.array(invitationSchema), nextCursor: z.string().optional() }).strict()
export const invitationResponseSchema = z.object({ invitation: invitationSchema }).strict()

export const setupStatusSchema = z
  .object({ status: z.enum(['required', 'complete']) })
  .strict()

export type SetupStatus = z.infer<typeof setupStatusSchema>

export const setupResponseSchema = z
  .object({ status: z.literal('complete') })
  .strict()

export const setupInputSchema = z
  .object({
    token: z.string(),
    name: z.string(),
    email: z.string(),
    password: z.string(),
    locale: z.enum(['en', 'zh-CN']).optional(),
  })
  .strict()

export const loginInputSchema = z
  .object({ email: z.string(), password: z.string(), locale: z.enum(['en', 'zh-CN']).optional() })
  .strict()

export const passwordResetRequestInputSchema = z
  .object({ email: z.string() })
  .strict()

export const passwordResetCompleteInputSchema = z
  .object({ token: z.string(), password: z.string() })
  .strict()

export const roleMutationInputSchema = z.object({
  name: z.string(),
  description: z.string(),
  permissions: z.array(z.string()),
  revision: z.number().int().nonnegative().optional(),
}).strict()
export const assignmentInputSchema = z.object({ roleIds: z.array(z.string().uuid()), authVersion: z.number().int().positive() }).strict()
export const invitationInputSchema = z.object({ name: z.string(), email: z.string(), roleIds: z.array(z.string().uuid()) }).strict()
export const invitationAcceptanceInputSchema = z.object({ token: z.string(), password: z.string() }).strict()

export const emailSettingsSchema = z.object({
  configured: z.boolean(),
  host: z.string().optional(),
  port: z.number().int().optional(),
  security: z.enum(['none', 'starttls', 'tls']).optional(),
  username: z.string().optional(),
  passwordSet: z.boolean(),
  fromAddress: z.string().optional(),
  fromName: z.string().optional(),
  defaultLocale: z.enum(['en', 'zh-CN']).optional(),
  autoRetryCount: z.number().int().nonnegative().optional(),
  retentionDays: z.number().int().positive().optional(),
  revision: z.number().int().nonnegative(),
  updatedAt: z.string().optional(),
}).strict()
export type EmailSettings = z.infer<typeof emailSettingsSchema>
export const emailSettingsResponseSchema = z.object({ email: emailSettingsSchema }).strict()

export const emailTaskStatusSchema = z.enum(['queued', 'sending', 'waiting_retry', 'sent', 'failed'])
export const emailTaskKindSchema = z.enum(['password_reset', 'password_changed', 'user_invitation', 'email_change_code', 'email_changed', 'test_email'])
export const emailTaskAttemptSchema = z.object({
  id: z.string().uuid().optional(),
  round: z.number().int().positive().optional(),
  attempt: z.number().int().positive().optional(),
  outcome: z.enum(['sent', 'failed']),
  errorCode: z.string().optional(),
  occurredAt: z.string().datetime(),
}).strict()
export const emailTaskSchema = z.object({
  id: z.string().uuid(),
  kind: emailTaskKindSchema,
  purpose: z.string().optional(),
  recipientEmail: z.string(),
  recipientName: z.string().optional(),
  locale: z.enum(['en', 'zh-CN']),
  status: emailTaskStatusSchema,
  createdAt: z.string().datetime(),
  availableAt: z.string().datetime().optional(),
  finishedAt: z.string().datetime().optional(),
  attemptCount: z.number().int().nonnegative(),
  round: z.number().int().positive().optional(),
  roundAttemptCount: z.number().int().nonnegative().optional(),
  lastErrorCode: z.string().optional(),
  attempts: z.array(emailTaskAttemptSchema).optional(),
}).strict()
export type EmailTask = z.infer<typeof emailTaskSchema>
export const emailTasksResponseSchema = z.object({ tasks: z.array(emailTaskSchema), nextCursor: z.string().optional() }).strict()
export const emailTaskResponseSchema = z.object({ task: emailTaskSchema }).strict()
export const emailTaskBulkItemSchema = z.object({ id: z.string().uuid(), result: z.enum(['succeeded', 'skipped', 'failed']), code: z.string().optional() }).strict()
export const emailTaskBulkResponseSchema = z.object({ succeeded: z.number().int().nonnegative(), skipped: z.number().int().nonnegative(), failed: z.number().int().nonnegative(), items: z.array(emailTaskBulkItemSchema) }).strict()
export const submittedEmailTaskResponseSchema = z.object({ status: z.literal('accepted'), task: emailTaskSchema.optional() }).strict()
export const operationalWarningsSchema = z.object({ warnings: z.array(z.object({ key: z.string(), severity: z.string() }).strict()) }).strict()

export const systemIdentitySchema = z.object({
  systemName: z.string(),
  iconUrl: z.string(),
  hasCustomIcon: z.boolean(),
  revision: z.number().int().nonnegative(),
  updatedAt: z.string().optional(),
}).strict()
export type SystemIdentity = z.infer<typeof systemIdentitySchema>
export const systemIdentityResponseSchema = systemIdentitySchema

export const operationActorSchema = z.object({ deleted: z.boolean().optional(), id: z.string().optional(), name: z.string().optional(), email: z.string().optional(), kind: z.string().optional(), label: z.string().optional() }).strict()
export const operationLogSchema = z.object({
  id: z.string().uuid(),
  actor: operationActorSchema,
  action: z.string(),
  objectType: z.string(),
  objectId: z.string().optional(),
  objectDeleted: z.boolean().optional(),
  result: z.enum(['success', 'failure']),
  occurredAt: z.string(),
  sourceIp: z.string().optional(),
  attemptedAccount: z.string().optional(),
  details: z.record(z.string(), z.unknown()),
}).strict()
export type OperationLog = z.infer<typeof operationLogSchema>
export const operationLogsResponseSchema = z.object({ logs: z.array(operationLogSchema), nextCursor: z.string().optional() }).strict()
export const operationLogResponseSchema = z.object({ log: operationLogSchema }).strict()
export const operationLogStatusSchema = z.object({ state: z.enum(['unknown', 'healthy', 'failed', 'recovered']), failureCount: z.number().int().nonnegative(), lastFailureAt: z.string().optional(), lastSuccessAt: z.string().optional() }).strict()
export const operationLogRetentionSchema = z.object({ retentionDays: z.number().int(), revision: z.number().int().nonnegative(), updatedAt: z.string().optional() }).strict()

export const passwordResetAcceptedSchema = z
  .object({ status: z.literal('accepted') })
  .strict()

export const sessionStatusSchema = z
  .object({ status: z.literal('ok') })
  .strict()

export const fieldProblemSchema = z
  .object({
    pointer: z.string(),
    code: z.string(),
    params: z.record(z.string(), z.unknown()).optional(),
  })
  .passthrough()

export const problemDetailsSchema = z
  .object({
    type: z.string(),
    title: z.string(),
    status: z.number().int(),
    code: z.string().optional(),
    detail: z.string().optional(),
    instance: z.string().optional(),
    errors: z.array(fieldProblemSchema).optional(),
  })
  .passthrough()

export type ProblemDetails = z.infer<typeof problemDetailsSchema>
export type FieldProblem = z.infer<typeof fieldProblemSchema>

export const onlineUsersResponseSchema = z.object({ users: z.array(z.object({
  id: z.string().uuid(), name: z.string(), email: z.string(), avatarUrl: z.string().optional(), hasAvatar: z.boolean().default(false), avatarVersion: z.number().int().nonnegative().optional(),
  lastSeenAt: z.string().datetime(), sessionCount: z.number().int().positive(),
}).strict()) }).strict()

export const emailChangeSchema = z.object({
  id: z.string().uuid(),
  oldEmail: z.string(),
  newEmail: z.string(),
  expiresAt: z.string().datetime(),
  resendAvailableAt: z.string().datetime(),
  attemptsRemaining: z.number().int().nonnegative(),
  revision: z.number().int().positive(),
}).strict()
export type EmailChange = z.infer<typeof emailChangeSchema>
export const personalProfileResponseSchema = z.object({ user: userSchema, emailChange: emailChangeSchema.nullable().optional() }).strict()
export const emailChangeResponseSchema = z.object({ emailChange: emailChangeSchema }).strict()
export const personalEmailChangeStatusSchema = z.object({ emailChange: emailChangeSchema.nullable() }).strict()
export const personalPasswordInputSchema = z.object({ currentPassword: z.string(), newPassword: z.string(), confirmPassword: z.string() }).strict()
export const personalEmailChangeInputSchema = z.object({ currentPassword: z.string(), newEmail: z.string() }).strict()
export const personalEmailVerifyInputSchema = z.object({ requestId: z.string().uuid(), code: z.string() }).strict()
export type OnlineUser = z.infer<typeof onlineUsersResponseSchema>['users'][number]
