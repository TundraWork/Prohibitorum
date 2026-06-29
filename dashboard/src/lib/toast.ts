/**
 * toast — framework-agnostic reactive toast queue (NOT a Pinia store).
 *
 * A module singleton so it can be driven from outside the component tree
 * (src/lib/api.ts connection-error handler, main.ts wiring) as well as from
 * components. The `toasts` array is a Vue `reactive` so the <Toaster/> overlay
 * re-renders on push/dismiss.
 *
 * Dedup: a `key` collapses repeated pushes (e.g. ten simultaneous failing
 * requests all signalling "server unreachable") into a single visible toast
 * whose auto-dismiss timer is refreshed, instead of stacking ten cards.
 */
import { reactive } from 'vue'

export type ToastVariant = 'error' | 'info' | 'success'

export interface Toast {
  id: number
  variant: ToastVariant
  message: string
  title?: string
}

export const toasts = reactive<Toast[]>([])

let nextId = 1
// key → currently-showing toast id (for dedup)
const keyToId = new Map<string, number>()
// id → auto-dismiss timer handle
const idToTimer = new Map<number, ReturnType<typeof setTimeout>>()

/**
 * Show a toast. `timeoutMs` defaults to 6000; `0` makes it sticky (no
 * auto-dismiss). If `key` matches an already-showing toast, no second toast is
 * added — its auto-dismiss timer is refreshed and the existing id is returned.
 * Returns the toast id.
 */
export function pushToast(opts: {
  variant: ToastVariant
  message: string
  title?: string
  key?: string
  timeoutMs?: number
}): number {
  const { variant, message, title, key, timeoutMs = 6000 } = opts

  // Dedup: refresh the existing toast's timer instead of stacking a duplicate.
  if (key !== undefined && keyToId.has(key)) {
    const existingId = keyToId.get(key)!
    const oldTimer = idToTimer.get(existingId)
    if (oldTimer !== undefined) clearTimeout(oldTimer)
    idToTimer.delete(existingId)
    if (timeoutMs > 0) {
      idToTimer.set(existingId, setTimeout(() => dismissToast(existingId), timeoutMs))
    }
    return existingId
  }

  const id = nextId++
  toasts.push({ id, variant, message, title })
  if (key !== undefined) keyToId.set(key, id)
  if (timeoutMs > 0) {
    idToTimer.set(id, setTimeout(() => dismissToast(id), timeoutMs))
  }
  return id
}

/** Remove a toast by id, clearing its timer and any key mapping. */
export function dismissToast(id: number): void {
  const idx = toasts.findIndex((t) => t.id === id)
  if (idx !== -1) toasts.splice(idx, 1)

  const timer = idToTimer.get(id)
  if (timer !== undefined) clearTimeout(timer)
  idToTimer.delete(id)

  for (const [k, v] of keyToId.entries()) {
    if (v === id) {
      keyToId.delete(k)
      break
    }
  }
}

/** Remove every toast (used by tests for isolation). */
export function clearToasts(): void {
  for (const timer of idToTimer.values()) clearTimeout(timer)
  idToTimer.clear()
  keyToId.clear()
  toasts.splice(0, toasts.length)
}
