import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { ReleaseConsole } from '@/features/release/release-console'

export const Route = createFileRoute('/_authenticated/')({ component: ReleaseConsoleRoute })

function ReleaseConsoleRoute() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  return <ReleaseConsole user={user} />
}
