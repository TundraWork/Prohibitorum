import { isCancelledError } from '@tanstack/vue-query'

export function isRequestCancelled(error: unknown): boolean {
  return isCancelledError(error) || (error instanceof DOMException && error.name === 'AbortError')
}
