import { createRouter } from '@tanstack/react-router'
import { basePath } from '@/lib/base-path'
import { routeTree } from '@/routeTree.gen'
import type { AppRouterContext } from './context'

export function createAppRouter(context: AppRouterContext) {
  return createRouter({
    routeTree,
    context,
    basepath: basePath || '/',
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
