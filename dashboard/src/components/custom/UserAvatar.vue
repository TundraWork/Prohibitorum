<script setup lang="ts">
/** UserAvatar — image (src) → initials → generic-icon fallback. */
import { computed, ref, watch } from 'vue'
import { User, Loader2 } from 'lucide-vue-next'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  displayName?: string | null
  username?: string | null
  size?: 'sm' | 'md'
  src?: string | null
  loading?: boolean
}>(), { size: 'md' })

const failed = ref(false)
watch(() => props.src, () => { failed.value = false })
const showImg = computed(() => !!props.src && !failed.value)

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
  <!-- aria-hidden: the avatar is decorative; the loading overlay is a visual-only indicator, not announced -->
  <span
    aria-hidden="true"
    :class="cn('relative inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-sidebar-accent font-medium text-sidebar-accent-foreground', sizeClass)"
  >
    <img v-if="showImg" :src="src!" alt="" loading="lazy" class="size-full object-cover" @error="failed = true" />
    <template v-else-if="initials">{{ initials }}</template>
    <User v-else class="size-4" />
    <span v-if="loading" data-test="avatar-spinner" class="absolute inset-0 flex items-center justify-center bg-black/40">
      <Loader2 class="size-3 animate-spin text-white" />
    </span>
  </span>
</template>
