import { createFileRoute, useLoaderData } from '@tanstack/react-router'
import { ReleaseConsole } from '@/features/release/release-console'

type ReleaseSearch = { job?: string }

export const Route = createFileRoute('/_authenticated/release')({
  validateSearch: (search: Record<string, unknown>): ReleaseSearch => (typeof search.job === 'string' && search.job ? { job: search.job } : {}),
  component: ReleaseRoute,
})

function ReleaseRoute() {
  const { user } = useLoaderData({ from: '/_authenticated' })
  const { job } = Route.useSearch()
  const navigate = Route.useNavigate()
  return <ReleaseConsole user={user} jobID={job} onJob={(next) => void navigate({ search: { job: next } })} />
}
