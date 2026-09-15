import { ref } from 'vue'
import { api } from './api'

/**
 * Sudo step-up gate (singleton). The SudoModal — mounted once in
 * DashboardLayout — watches `sudoState`; withSudo()/ensureSudo() open it and
 * await the user's ceremony. Backend contract: sensitive /me actions return
 * {code:'sudo_required'} until the session is within the recent-auth window
 * (granted at login) or holds a fresh step-up grant. The grant is multi-use
 * until it expires.
 */
export interface SudoState {
  open: boolean
  resolve: ((ok: boolean) => void) | null
  reason?: string
}
export const sudoState = ref<SudoState>({ open: false, resolve: null })

/** Check the server's current grant before a redirect that cannot use XHR retry. */
export async function ensureSudo(reason?: string): Promise<boolean> {
  const state = await api.get<{ fresh: boolean }>('/api/prohibitorum/me/sudo/methods')
  if (state.fresh === true) return true
  return promptSudo(reason)
}

/** Open the step-up modal; resolves true (elevated) / false (cancelled). */
function promptSudo(reason?: string): Promise<boolean> {
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
    const ok = await promptSudo(reason)
    if (!ok) throw e
    return await fn()
  }
}
