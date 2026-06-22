<script setup lang="ts">
/** AppIcon — an app/provider icon: image (src) → initial-letter fallback. */
import { computed, ref, watch } from 'vue'
import { cn } from '@/lib/utils'

const props = withDefaults(defineProps<{
  src?: string | null
  name?: string | null
  size?: 'sm' | 'md' | 'lg'
}>(), { size: 'md' })

const failed = ref(false)
watch(() => props.src, () => { failed.value = false })
const showImg = computed(() => !!props.src && !failed.value)

const initial = computed(() => {
  const n = (props.name ?? '').trim()
  return n ? n[0]!.toUpperCase() : '?'
})

const sizeClass = computed(() => ({
  sm: 'size-6 text-xs',
  md: 'size-10 text-base',
  lg: 'size-16 text-2xl',
}[props.size]))
</script>

<template>
  <span
    aria-hidden="true"
    :class="cn('inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md bg-accent font-semibold text-ink', sizeClass)"
  >
    <img v-if="showImg" :src="src!" alt="" loading="lazy" class="size-full object-cover" @error="failed = true" />
    <template v-else>{{ initial }}</template>
  </span>
</template>
