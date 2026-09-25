import { useQuery } from '@tanstack/react-query'
import { createContext, useContext, useEffect } from 'react'
import { Layers3 } from 'lucide-react'
import type { ApiClient } from '@/shared/api/client'
import type { SystemIdentity } from '@/shared/api/contracts'

export const publicSystemIdentityQueryKey = ['public', 'system-identity'] as const
export const defaultSystemIdentity: SystemIdentity = {
  systemName: 'Temvia',
  iconUrl: '/api/public/system-identity/icon?default=1&style=layers',
  hasCustomIcon: false,
  revision: 0,
}

export const SystemIdentityApiContext = createContext<ApiClient | undefined>(undefined)

export function DefaultSystemIcon({ className }: { className?: string }) {
  return <svg aria-hidden="true" data-system-icon="default" viewBox="0 0 64 64" className={className}>
    <rect width="64" height="64" rx="16" fill="#18181b" />
    <Layers3 x="16" y="16" width="32" height="32" color="white" strokeWidth={1.8} />
  </svg>
}

export function systemIdentityName(identity: SystemIdentity | undefined): string {
  return identity?.systemName.trim() || defaultSystemIdentity.systemName
}

export function usePublicSystemIdentity(api: ApiClient) {
  return useQuery({
    queryKey: publicSystemIdentityQueryKey,
    queryFn: ({ signal }) => api.getPublicSystemIdentity ? api.getPublicSystemIdentity(signal) : Promise.resolve(defaultSystemIdentity),
    staleTime: 0,
    refetchOnWindowFocus: true,
    retry: false,
  })
}

export function IdentityMark({ api, compact = false }: { api?: ApiClient; compact?: boolean }) {
  const contextApi = useContext(SystemIdentityApiContext)
  if (!api && !contextApi) return <IdentityMarkView identity={defaultSystemIdentity} compact={compact} />
  return <IdentityMarkWithQuery api={api ?? contextApi!} compact={compact} />
}

function IdentityMarkWithQuery({ api, compact }: { api: ApiClient; compact: boolean }) {
  const query = usePublicSystemIdentity(api)
  const identity = query.data ?? defaultSystemIdentity
  return <IdentityMarkView identity={identity} compact={compact} />
}

function IdentityMarkView({ identity, compact }: { identity: SystemIdentity; compact: boolean }) {
  const name = systemIdentityName(identity)
  return (
    <div className={`flex min-w-0 items-center gap-3 ${compact ? 'max-w-48' : ''}`}>
      {identity.hasCustomIcon
        ? <img src={identity.iconUrl} alt="" className={`${compact ? 'size-8 rounded-md' : 'size-10 rounded-lg'} shrink-0 object-contain`} />
        : <DefaultSystemIcon className={`${compact ? 'size-8' : 'size-10'} shrink-0`} />}
      <span className="min-w-0 truncate text-sm font-semibold tracking-tight">{name}</span>
    </div>
  )
}

export function SystemIdentityRuntime({ api }: { api: ApiClient }) {
  const query = usePublicSystemIdentity(api)
  const identity = query.data ?? defaultSystemIdentity
  const name = systemIdentityName(identity)

  useEffect(() => {
    if (typeof document === 'undefined') return
    document.title = name
    let link = document.head.querySelector<HTMLLinkElement>('link[data-temvia-system-icon]')
    if (!link) {
      link = document.createElement('link')
      link.dataset.temviaSystemIcon = 'true'
      link.rel = 'icon'
      document.head.append(link)
    }
    link.href = identity.iconUrl
    link.type = identity.hasCustomIcon ? 'image/png' : 'image/svg+xml'
    const description = document.head.querySelector<HTMLMetaElement>('meta[name="description"]')
    if (description) description.content = `${name} administration`
  }, [identity.hasCustomIcon, identity.iconUrl, name])

  return null
}
