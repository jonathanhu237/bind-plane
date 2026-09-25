import { render, waitFor } from '@testing-library/react'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { readFailureFeedback, useRequestErrorToast } from './feedback'
import { ApiProblemError } from '@/shared/api/client'

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

function ErrorObserver({ error }: { error: unknown }) {
  useRequestErrorToast(error, true, () => '', { title: 'Request failed', description: 'Refresh the page.' })
  return null
}

describe('request feedback', () => {
  beforeEach(() => vi.clearAllMocks())

  it('announces the same failed request once across observers', async () => {
    const error = new Error('network')
    render(<><ErrorObserver error={error} /><ErrorObserver error={error} /></>)

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Request failed', { description: 'Refresh the page.' }))
    expect(toast.error).toHaveBeenCalledOnce()
  })

  it('uses an access-denied message for forbidden reads', () => {
    const copy = readFailureFeedback(new ApiProblemError({ type: '/problems/forbidden', title: 'forbidden', status: 403, code: 'forbidden' }), {
      unavailableTitle: 'Unavailable',
      unavailableDescription: 'Refresh the page.',
      forbiddenTitle: 'Access denied',
      forbiddenDescription: 'You do not have access.',
    })

    expect(copy).toEqual({ title: 'Access denied', description: 'You do not have access.' })
  })
})
