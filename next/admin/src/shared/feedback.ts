import { useEffect, useRef } from 'react'
import { toast } from 'sonner'
import { isForbidden, isRequestCancelled, translateProblemWithFields } from '@/shared/api/problems'

type Translator = unknown

// Query observers can subscribe to the same failed request. Keep the error
// object in a weak set so one failure is announced once without retaining it.
const announcedRequestErrors = new WeakSet<object>()

export type RequestFeedbackOptions = {
  title?: string
  description?: string
}

export type ReadFailureFeedbackOptions = {
  unavailableTitle: string
  unavailableDescription: string
  forbiddenTitle: string
  forbiddenDescription: string
}

export function readFailureFeedback(error: unknown, options: ReadFailureFeedbackOptions): RequestFeedbackOptions {
  return isForbidden(error)
    ? { title: options.forbiddenTitle, description: options.forbiddenDescription }
    : { title: options.unavailableTitle, description: options.unavailableDescription }
}

/** Show one request failure without exposing raw server or transport details. */
export function notifyRequestError(error: unknown, t: Translator, options: RequestFeedbackOptions = {}): void {
  if (isRequestCancelled(error)) return
  const description = options.description ?? translateProblemWithFields(error, t)
  if (options.title) {
    toast.error(options.title, { description })
    return
  }
  toast.error(description)
}

export function notifySuccess(message: string): void {
  toast.success(message)
}

function notifyRequestErrorOnce(error: unknown, t: Translator, options: RequestFeedbackOptions): void {
  if (typeof error === 'object' && error !== null) {
    if (announcedRequestErrors.has(error)) return
    announcedRequestErrors.add(error)
  }
  notifyRequestError(error, t, options)
}

/**
 * Announce a query failure once for the current error object. React Query may
 * render the same error more than once while preserving its cached result.
 */
export function useRequestErrorToast(error: unknown, enabled: boolean, t: Translator, options: RequestFeedbackOptions = {}): void {
  const announced = useRef<unknown>(undefined)
  const { title, description } = options
  useEffect(() => {
    if (!enabled || error === undefined || error === null || error === announced.current) return
    announced.current = error
    notifyRequestErrorOnce(error, t, { title, description })
  }, [description, enabled, error, t, title])
}
