import { queryOptions } from '@tanstack/react-query'
import { ApiProtocolError, type ApiClient } from '@/shared/api/client'

export const rolesQueryKey = ['access', 'roles'] as const
export const roleQueryKey = (id: string) => ['access', 'roles', id] as const
export type AccessListOptions = { cursor?: string; q?: string; roleId?: string; status?: 'active' | 'disabled' | 'pending' | 'expired'; sort?: string; direction?: 'asc' | 'desc' }
export const usersQueryKey = (options: AccessListOptions = {}) => ['access', 'users', options.cursor ?? '', options.q ?? '', options.roleId ?? '', options.status ?? '', options.sort ?? '', options.direction ?? ''] as const
export const invitationsQueryKey = (options: AccessListOptions = {}) => ['access', 'invitations', options.cursor ?? '', options.q ?? '', options.roleId ?? '', options.status ?? '', options.sort ?? '', options.direction ?? ''] as const
export const roleOptionsQueryKey = ['access', 'role-options'] as const

const missingMethod = () => Promise.reject(new ApiProtocolError('This API client does not expose access management.'))

export function rolesOptions(api: ApiClient) {
  return queryOptions({
    queryKey: rolesQueryKey,
    queryFn: ({ signal }) => api.getRoles ? api.getRoles(signal) : missingMethod(),
    retry: false,
    staleTime: 10_000,
  })
}

export function roleOptionsOptions(api: ApiClient) {
  return queryOptions({
    queryKey: roleOptionsQueryKey,
    queryFn: ({ signal }) => api.getRoleOptions ? api.getRoleOptions(signal) : missingMethod(),
    retry: false,
    staleTime: 30_000,
  })
}

export function roleOptions(api: ApiClient, id: string) {
  return queryOptions({
    queryKey: roleQueryKey(id),
    queryFn: ({ signal }) => api.getRole ? api.getRole(id, signal) : missingMethod(),
    retry: false,
  })
}

export function usersOptions(api: ApiClient, options: AccessListOptions = {}) {
  return queryOptions({
    queryKey: usersQueryKey(options),
    queryFn: ({ signal }) => api.getUsers ? api.getUsers({ cursor: options.cursor || undefined, q: options.q || undefined, roleId: options.roleId || undefined, status: options.status as 'active' | 'disabled' | undefined, sort: options.sort, direction: options.direction }, signal) : missingMethod(),
    retry: false,
  })
}

export function invitationsOptions(api: ApiClient, options: AccessListOptions = {}) {
  return queryOptions({
    queryKey: invitationsQueryKey(options),
    queryFn: ({ signal }) => api.getInvitations ? api.getInvitations({ cursor: options.cursor || undefined, q: options.q || undefined, roleId: options.roleId || undefined, status: options.status === 'pending' || options.status === 'expired' ? options.status : undefined, sort: options.sort, direction: options.direction }, signal) : missingMethod(),
    retry: false,
  })
}
