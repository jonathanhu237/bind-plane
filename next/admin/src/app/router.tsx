import { createRouter } from '@tanstack/react-router'
import { routeTree } from '@/routeTree.gen'
import type { AppRouterContext } from './context'

export function createAppRouter(context: AppRouterContext) {
  return createRouter({
    routeTree,
    context,
    defaultPreload: 'intent',
    defaultStructuralSharing: true,
  })
}

export type AppRouter = ReturnType<typeof createAppRouter>

declare module '@tanstack/react-router' {
  interface Register {
    router: AppRouter
  }
}
