import { createFileRoute, redirect } from '@tanstack/react-router'

// Asset settings became a sidebar group with a page each; old links land on the first of them.
export const Route = createFileRoute('/_authenticated/assets')({
  beforeLoad: () => { throw redirect({ to: '/credentials', replace: true }) },
})
