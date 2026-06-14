/**
 * Auth store — session/me state.
 *
 * `SessionView` shape matches pkg/contract/auth.go:
 *   { id: int32, username: string, displayName: string, role: string, attributes?: map }
 *
 * `ensureLoaded()` fetches /me once (memoized by `_loaded`). A 401 is treated
 * as "not authenticated" → me=null, not an error. Any other API error is
 * re-thrown so callers can surface it if needed.
 *
 * `isAdmin` is a computed shorthand — the guard composable (Task 3 router) uses
 * it rather than inline role comparisons in every component.
 */

import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '@/lib/api'
import type { ApiError } from '@/lib/api'

export interface SessionView {
  id: number
  username: string
  displayName: string
  role: string
  attributes?: Record<string, unknown>
  avatarUrl?: string
  avatarPending?: boolean
  avatarSource?: string
  avatarSourceUrls?: Record<string, string>
}

export const useAuthStore = defineStore('auth', () => {
  const me = ref<SessionView | null>(null)
  const _loaded = ref(false)

  const isAdmin = computed(() => me.value?.role === 'admin')

  async function ensureLoaded(): Promise<void> {
    if (_loaded.value) return
    try {
      me.value = await api.get<SessionView>('/api/prohibitorum/me')
    } catch (err: unknown) {
      // 401 = unauthenticated; treat as null session, not an error
      if ((err as ApiError).code === 'server_error') {
        // Non-JSON errors from the server on /me are unexpected; re-throw.
        // A clean 401 from the API will have code: 'unauthorized' or similar,
        // which we handle as unauthenticated below.
      }
      const apiErr = err as ApiError
      // Treat 4xx responses (including 401 unauthorized) as "not logged in"
      if (apiErr && typeof apiErr.code === 'string') {
        me.value = null
      } else {
        // Network error or unexpected — re-throw
        throw err
      }
    } finally {
      _loaded.value = true
    }
  }

  async function reload(): Promise<void> { _loaded.value = false; await ensureLoaded() }

  function setDisplayName(name: string): void {
    if (me.value) me.value = { ...me.value, displayName: name }
  }

  function clear(): void {
    me.value = null
    _loaded.value = false
  }

  let _pollActive = false
  function pollAvatarUntilSettled(): () => void {
    if (!me.value?.avatarPending || _pollActive) return () => {}
    _pollActive = true
    let cancelled = false
    const stop = () => { cancelled = true; _pollActive = false }
    const tick = async (): Promise<void> => {
      if (cancelled) return
      try {
        const res = await api.get<{ pending: boolean }>('/api/prohibitorum/me/avatar/status')
        if (res.pending) { if (!cancelled) setTimeout(() => { void tick() }, 1500); return }
      } catch {
        // Terminal: stop polling and clear the pending flag client-side so the
        // spinner never sticks (the avatar refreshes on the next natural /me load).
        if (me.value) me.value.avatarPending = false
        _pollActive = false
        return
      }
      if (!cancelled) { _pollActive = false; await reload() }
    }
    setTimeout(() => { void tick() }, 1500)
    return stop
  }

  return { me, isAdmin, ensureLoaded, reload, setDisplayName, clear, pollAvatarUntilSettled }
})
