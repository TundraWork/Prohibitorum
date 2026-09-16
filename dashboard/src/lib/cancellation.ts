export function isRequestCancelled(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError'
}
