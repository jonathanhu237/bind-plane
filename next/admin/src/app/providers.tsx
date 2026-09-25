import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import { Toaster } from '@/components/ui/sonner'
import type { QueryClient } from '@tanstack/react-query'
import type { AppRouter } from './router'
import { ThemeProvider, useTheme } from '@/shared/theme'
import { SystemIdentityApiContext, SystemIdentityRuntime } from '@/features/identity/system-identity'
import type { ApiClient } from '@/shared/api/client'

function ThemeToaster() {
  const { resolvedTheme } = useTheme()
  return <Toaster theme={resolvedTheme} />
}

export function AppProviders({ queryClient, router, api }: { queryClient: QueryClient; router: AppRouter; api: ApiClient }) {
  return (
    <ThemeProvider>
      <QueryClientProvider client={queryClient}>
        <SystemIdentityApiContext.Provider value={api}>
          <SystemIdentityRuntime api={api} />
        <ThemeToaster />
        <RouterProvider router={router} />
        </SystemIdentityApiContext.Provider>
      </QueryClientProvider>
    </ThemeProvider>
  )
}
