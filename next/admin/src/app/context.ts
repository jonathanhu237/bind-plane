import type { QueryClient } from '@tanstack/react-query'
import type { ApiClient } from '@/shared/api/client'

export interface AppRouterContext {
  api: ApiClient
  queryClient: QueryClient
}
