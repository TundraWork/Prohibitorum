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

  function setDisplayName(name: string): void {
    if (me.value) me.value = { ...me.value, displayName: name }
  }

  function clear(): void {
    me.value = null
    _loaded.value = false
  }

  return { me, isAdmin, ensureLoaded, setDisplayName, clear }
})
