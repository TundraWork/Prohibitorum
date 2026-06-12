<script setup lang="ts">
/** UserAvatar — initials box (displayName → username), generic-icon fallback. */
import { computed } from 'vue'
import { User } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  displayName?: string | null
  username?: string | null
  size?: 'sm' | 'md'
}>(), { size: 'md' })

const initials = computed(() => {
  const name = (props.displayName ?? '').trim()
  if (name) {
    const parts = name.split(/\s+/).filter(Boolean)
    const chars = parts.length >= 2 ? parts[0][0] + parts[parts.length - 1][0] : parts[0].slice(0, 2)
    return chars.toUpperCase()
  }
  const u = (props.username ?? '').trim()
  if (u) return u.slice(0, 2).toUpperCase()
  return ''
})

const sizeClass = computed(() => (props.size === 'sm' ? 'size-6 text-[0.625rem]' : 'size-8 text-xs'))
</script>

<template>
  <span
    aria-hidden="true"
    :class="cn('inline-flex shrink-0 items-center justify-center rounded-md bg-sidebar-accent font-medium text-sidebar-accent-foreground', sizeClass)"
  >
    <template v-if="initials">{{ initials }}</template>
    <User v-else class="size-4" />
  </span>
</template>
