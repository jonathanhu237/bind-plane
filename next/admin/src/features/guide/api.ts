import { withBase } from '@/lib/base-path'
import { request } from '@/features/release/jobs'

/** The newest version of the guided tour the signed-in user has finished or skipped; 0 for none. */
export type GuideState = { seen_version: number }

export const guideQueryKey = (userID: string) => ['guide', userID] as const

export const readGuide = () => request<GuideState>('/api/releases/guide')

/** Remembers on the server, not in this browser, that the user has seen this version of the tour. */
export async function seeGuide(version: number): Promise<void> {
  const response = await fetch(withBase('/api/releases/guide'), {
    method: 'PUT', credentials: 'same-origin', cache: 'no-store',
    headers: { Accept: 'application/json', 'Content-Type': 'application/json' },
    body: JSON.stringify({ version }),
  })
  if (!response.ok) throw new Error(`guide not saved: ${response.status}`)
}
