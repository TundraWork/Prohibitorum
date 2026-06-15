import { ref } from 'vue'

/**
 * Sudo step-up gate (singleton). The SudoModal — mounted once in
 * DashboardLayout — watches `sudoState`; withSudo()/ensureSudo() open it and
 * await the user's ceremony. Backend contract: sensitive /me actions return
 * {code:'sudo_required'} until the session has a fresh (one-shot) sudo grant.
 */
export interface SudoState {
  open: boolean
  resolve: ((ok: boolean) => void) | null
  reason?: string
}
export const sudoState = ref<SudoState>({ open: false, resolve: null })

/** Open the step-up modal; resolves true (elevated) / false (cancelled). */
export function ensureSudo(reason?: string): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    sudoState.value = { open: true, resolve, reason }
  })
}

/** Test/internal hook: resolve the pending sudo promise and close the modal. */
export function _resolveSudo(ok: boolean): void {
  const r = sudoState.value.resolve
  sudoState.value = { open: false, resolve: null }
  r?.(ok)
}

/** Run fn(); if it fails with sudo_required, step up and retry once. */
export async function withSudo<T>(fn: () => Promise<T>, reason?: string): Promise<T> {
  try {
    return await fn()
  } catch (e: unknown) {
    if ((e as { code?: string })?.code !== 'sudo_required') throw e
    const ok = await ensureSudo(reason)
    if (!ok) throw e
    return await fn()
  }
}
