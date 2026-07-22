<script setup lang="ts">
/**
 * AppIcon — the single app / provider / identity icon primitive.
 *
 * Render priority: a bundled BRAND mark for a known protocol (steam/vrchat,
 * from providerBrand) → the uploaded IMAGE (src) → an INITIAL-letter fallback.
 *
 * The `<img>` inherits the container radius (`rounded-[inherit]`) so it is
 * clipped to the chip's rounded corners — without this a full-bleed image
 * leaves square corners that spill past a bordered chip (the "icon out of
 * border" bug). Pass `bordered` to draw the inset ring on the SAME element as
 * the clip, so the border and the rounded corners always coincide (callers must
 * not wrap AppIcon in their own ring span).
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
  bordered?: boolean
}>(), { size: 'md', bordered: false })

const failed = ref(false)
watch(() => props.src, () => { failed.value = false })

const brand = computed(() => providerBrand(props.protocol))
const showImg = computed(() => !brand.value && !!props.src && !failed.value)
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
      'inline-flex shrink-0 items-center justify-center overflow-hidden rounded-md font-semibold text-ink',
      brand ? '' : 'bg-accent',
      bordered && 'ring-1 ring-inset ring-border',
      sizeClass,
    )"
    :style="brand ? { backgroundColor: brand.bg } : undefined"
  >
    <img
      v-if="brand"
      :src="brand.logo"
      alt=""
      loading="lazy"
      class="size-[72%] object-contain"
    />
    <img
      v-else-if="showImg"
      :src="src!"
      alt=""
      loading="lazy"
      class="size-full rounded-[inherit] object-cover"
      @error="failed = true"
    />
    <template v-else>{{ initial }}</template>
  </span>
</template>
