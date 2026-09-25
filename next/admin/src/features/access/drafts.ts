import { create } from 'zustand'

export type RoleDraft = {
  id?: string
  revision?: number
  name: string
  description: string
  permissions: string[]
  /** Permissions explicitly selected by the administrator in this draft. */
  explicitPermissions?: string[]
  submitting: boolean
  conflict: boolean
  submissionID?: string
  validationError?: 'name' | 'permissions' | 'invalidPermissions'
}

export type InvitationDraft = {
  name: string
  email: string
  roleIDs: string[]
  submitting: boolean
  submissionID?: string
  validationError?: 'name' | 'email' | 'roles' | 'invalidRoles'
}

export type AssignmentDraft = {
  userID: string
  authVersion: number
  roleIDs: string[]
  submitting: boolean
  conflict: boolean
  submissionID?: string
  validationError: boolean
  invalidRoleSelection?: boolean
}

export type EmailSettingsDraft = {
  host: string
  port: number
  security: 'none' | 'starttls' | 'tls'
  authentication: boolean
  username: string
  password: string
  clearPassword?: boolean
  passwordSet: boolean
  fromAddress: string
  fromName: string
  defaultLocale?: 'en' | 'zh-CN'
  autoRetryCount: number
  retentionDays: number
  revision: number
  configured: boolean
  submitting: boolean
  conflict: boolean
}

export type OperationLogRetentionDraft = {
  ownerID?: string
  retentionDays: number
  revision: number
  submitting: boolean
  conflict: boolean
  authoritative?: boolean
  submissionID?: string
}

type DraftState = {
  ownerID?: string
  roleCreate?: RoleDraft
  roleEdits: Record<string, RoleDraft>
  invitation?: InvitationDraft
  assignments: Record<string, AssignmentDraft>
  emailSettings?: EmailSettingsDraft
  operationLogRetention?: OperationLogRetentionDraft
  setOwner: (ownerID: string) => void
  setRoleCreate: (draft: RoleDraft | undefined) => void
  setRoleEdit: (id: string, draft: RoleDraft | undefined) => void
  setInvitation: (draft: InvitationDraft | undefined) => void
  setAssignment: (id: string, draft: AssignmentDraft | undefined) => void
  setEmailSettings: (draft: EmailSettingsDraft | undefined) => void
  setOperationLogRetention: (draft: OperationLogRetentionDraft | undefined) => void
  clearAll: () => void
}

export const useAccessDraftStore = create<DraftState>((set) => ({
  roleEdits: {},
  assignments: {},
  setOwner: (ownerID) => set((state) => state.ownerID === undefined || state.ownerID === ownerID ? { ownerID } : { ownerID, roleCreate: undefined, roleEdits: {}, invitation: undefined, assignments: {}, emailSettings: undefined, operationLogRetention: undefined }),
  setRoleCreate: (roleCreate) => set({ roleCreate }),
  setRoleEdit: (id, draft) => set((state) => {
    const roleEdits = { ...state.roleEdits }
    if (draft === undefined) delete roleEdits[id]
    else roleEdits[id] = draft
    return { roleEdits }
  }),
  setInvitation: (invitation) => set({ invitation }),
  setAssignment: (id, draft) => set((state) => {
    const assignments = { ...state.assignments }
    if (draft === undefined) delete assignments[id]
    else assignments[id] = draft
    return { assignments }
  }),
  setEmailSettings: (emailSettings) => set({ emailSettings }),
  setOperationLogRetention: (operationLogRetention) => set({ operationLogRetention }),
  clearAll: () => set({ ownerID: undefined, roleCreate: undefined, roleEdits: {}, invitation: undefined, assignments: {}, emailSettings: undefined, operationLogRetention: undefined }),
}))

let submissionSequence = 0

/** Return a process-local identifier used to guard asynchronous draft updates. */
export function nextDraftSubmissionID(): string {
  submissionSequence += 1
  return `${Date.now().toString(36)}-${submissionSequence.toString(36)}`
}

export function clearAccessDrafts(): void {
  useAccessDraftStore.getState().clearAll()
}

export function clearEmailSettingsPasswordDraft(): void {
  const draft = useAccessDraftStore.getState().emailSettings
  if (draft) useAccessDraftStore.getState().setEmailSettings({ ...draft, password: '', clearPassword: false })
}
