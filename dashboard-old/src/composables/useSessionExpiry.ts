import { ref } from 'vue'

// Module-level singleton: the banner and the handler share one flag.
const expired = ref(false)

export function useSessionExpiry() {
  return {
    expired,
    trigger(): void { expired.value = true },
    reset(): void { expired.value = false },
  }
}
