import { MutationCache, QueryCache, QueryClient, type Mutation } from '@tanstack/react-query'
import { clearAccessDrafts } from '@/features/access/drafts'
import { isUnauthenticated } from '@/shared/api/problems'

function clearExpiredSession(getQueryClient: () => QueryClient | undefined, error: unknown): void {
  if (!isUnauthenticated(error)) return
  clearAccessDrafts()
  const queryClient = getQueryClient()
  queryClient?.removeQueries({ queryKey: ['auth', 'current-user'] })
  queryClient?.removeQueries({ queryKey: ['access'] })
  queryClient?.removeQueries({ queryKey: ['settings'] })
}

export function createAppQueryClient(): QueryClient {
  let queryClient: QueryClient | undefined
  const onQueryError = (error: unknown) => clearExpiredSession(() => queryClient, error)
  const onMutationError = (error: unknown, _variables: unknown, _context: unknown, mutation: Mutation<unknown, unknown, unknown, unknown>) => {
    if (mutation.options.meta?.preserveAccessDraftsOnError) return
    clearExpiredSession(() => queryClient, error)
  }
  queryClient = new QueryClient({
    queryCache: new QueryCache({ onError: onQueryError }),
    mutationCache: new MutationCache({ onError: onMutationError }),
    defaultOptions: {
      queries: {
        retry: false,
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  })
  return queryClient
}
