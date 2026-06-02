import { ref } from 'vue'

// Module singleton: the SudoModal (mounted once in DashboardLayout) watches this
// state; withSudo()/ensureSudo() open it and await the user's ceremony.
export interface SudoState { open: boolean; resolve: ((ok: boolean) => void) | null }
export const sudoState = ref<SudoState>({ open: false, resolve: null })

// Open the step-up modal and resolve true (succeeded) / false (cancelled).
export function ensureSudo(): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    sudoState.value = { open: true, resolve }
  })
}

// Test/internal hook: resolve the pending sudo promise and close the modal.
export function _resolveSudo(ok: boolean) {
  const r = sudoState.value.resolve
  sudoState.value = { open: false, resolve: null }
  r?.(ok)
}

// Run fn(); if it fails with sudo_required, step up and retry once.
export async function withSudo<T>(fn: () => Promise<T>): Promise<T> {
  try {
    return await fn()
  } catch (e: any) {
    if (e?.code !== 'sudo_required') throw e
    const ok = await ensureSudo()
    if (!ok) throw e
    return await fn()
  }
}
