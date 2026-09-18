import { ref, onUnmounted } from 'vue'

/**
 * useTransientFlag — a boolean flag that turns on via trigger() and auto-clears
 * after `ms`. For transient success confirmations ("Saved", "Rotated") that
 * should fade rather than linger. Re-triggering resets the timer.
 */
export function useTransientFlag(ms = 3000) {
  const flag = ref(false)
  let timer: ReturnType<typeof setTimeout> | null = null
  function clearTimer() { if (timer !== null) { clearTimeout(timer); timer = null } }
  function trigger() {
    flag.value = true
    clearTimer()
    timer = setTimeout(() => { flag.value = false; timer = null }, ms)
  }
  onUnmounted(clearTimer)
  return { flag, trigger }
}
