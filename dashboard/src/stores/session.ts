import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { api } from '../lib/api'

export interface SessionView {
  id: number
  username: string
  displayName: string
  role: string
}

export const useSessionStore = defineStore('session', () => {
  const me = ref<SessionView | null>(null)
  const loaded = ref(false)

  async function fetchMe(): Promise<SessionView | null> {
    try {
      me.value = await api.get<SessionView>('/api/prohibitorum/me')
    } catch {
      // 401 (or any error): treat as no live session.
      me.value = null
    }
    loaded.value = true
    return me.value
  }

  // Idempotent: fetch the session at most once. Used by the router guard and the
  // dashboard layout so they share one source of truth.
  async function ensureLoaded(): Promise<SessionView | null> {
    if (loaded.value) return me.value
    return fetchMe()
  }

  const isAdmin = computed(() => me.value?.role === 'admin')

  // Drop the cached session (after logout is initiated) so the next ensureLoaded refetches.
  function clear() {
    me.value = null
    loaded.value = false
  }

  return { me, fetchMe, ensureLoaded, isAdmin, clear }
})
