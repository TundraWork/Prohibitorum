<script setup lang="ts">
/**
 * AppIcon — app / provider / identity icon.
 *
 * Three states, deliberately styled differently:
 *  - UPLOADED image (src): shown object-contain at ~80% so it has margin on all
 *    sides, with NO background and NO crop — a transparent logo stays exactly
 *    that. (The image is never clipped to a frame; it just breathes.)
 *  - Known BRAND protocol (steam/vrchat): the brand mark, same margin, on the
 *    brand colour — Steam's white mark needs a dark backing to be visible.
 *  - PLACEHOLDER (no image): an initial letter on a neutral grey chip. This is
 *    the ONLY state with a filled background.
 */
import { computed, ref, watch } from 'vue'
import { cn } from '@/lib/utils'
import { providerBrand } from '@/lib/providerBrand'

const props = withDefaults(defineProps<{
  src?: string | null
  name?: string | null
  /** Upstream protocol — 'steam'/'vrchat' render their brand mark + colour. */
  protocol?: string | null
  size?: 'sm' | 'md' | 'lg'
}>(), { size: 'md' })

const failed = ref(false)
watch(() => props.src, () => { failed.value = false })

const brand = computed(() => providerBrand(props.protocol))
const showImg = computed(() => !brand.value && !!props.src && !failed.value)
const placeholder = computed(() => !brand.value && !showImg.value)
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
    :class="cn(
      'inline-flex shrink-0 items-center justify-center rounded-md font-semibold text-ink',
      placeholder && 'bg-accent',
      sizeClass,
    )"
    :style="brand ? { backgroundColor: brand.bg } : undefined"
  >
    <img
      v-if="brand"
      :src="brand.logo"
      alt=""
      loading="lazy"
      class="size-[80%] object-contain"
    />
    <img
      v-else-if="showImg"
      :src="src!"
      alt=""
      loading="lazy"
      class="size-[80%] object-contain"
      @error="failed = true"
    />
    <template v-else>{{ initial }}</template>
  </span>
</template>
