import { defineStore } from 'pinia'
import { ref } from 'vue'
import { api } from '../lib/api'

export interface SessionView {
  id: number
  username: string
  displayName: string
  role: string
}

export const useSessionStore = defineStore('session', () => {
  const me = ref<SessionView | null>(null)

  async function fetchMe(): Promise<SessionView | null> {
    try {
      me.value = await api.get<SessionView>('/api/prohibitorum/me')
    } catch {
      // 401 (or any error): treat as no live session.
      me.value = null
    }
    return me.value
  }

  return { me, fetchMe }
})
