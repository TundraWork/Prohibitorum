<script setup lang="ts">
/**
 * BrandButton — a full-width "sign in with <brand>" button for a protocol that
 * has a bundled brand identity (steam/vrchat), driven entirely by providerBrand
 * so the login page shares ONE source of brand colours + logo with the icon
 * chips (AppIcon) used elsewhere. Colours are injected as CSS custom properties
 * so Tailwind's `hover:` variants still resolve.
 *
 * Icon box (size-6) + gap + transparent 1px border match the generic outline
 * IdP button, so all sign-in buttons align icon-and-label pixel-for-pixel.
 */
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { providerBrand } from '@/lib/providerBrand'

const props = defineProps<{ protocol: string; label: string }>()
defineEmits<{ (e: 'click'): void }>()

const brand = computed(() => providerBrand(props.protocol))
const brandVars = computed(() =>
  brand.value
    ? {
        '--brand-bg': brand.value.bg,
        '--brand-hover-bg': brand.value.hoverBg,
        '--brand-fg': brand.value.fg,
        '--brand-hover-fg': brand.value.hoverFg ?? brand.value.fg,
      }
    : undefined,
)
</script>

<template>
  <Button
    v-if="brand"
    type="button"
    :data-test="`${protocol}-login`"
    :style="brandVars"
    class="w-full justify-start gap-2 border border-transparent bg-[var(--brand-bg)] text-[var(--brand-fg)] hover:bg-[var(--brand-hover-bg)] hover:text-[var(--brand-hover-fg)]"
    @click="$emit('click')"
  >
    <img :src="brand.logo" alt="" aria-hidden="true" class="size-6 rounded-md" />
    <span>{{ label }}</span>
  </Button>
</template>
