<script setup lang="ts">
/**
 * BrandButton — the "sign in / connect with <brand>" button for a protocol with
 * a bundled brand identity (steam/vrchat), driven by providerBrand so the login
 * page and the Connected Accounts page share ONE button style. Colours are CSS
 * custom properties so Tailwind's `hover:` variants resolve.
 *
 * The default slot holds optional trailing content (e.g. an "already linked"
 * badge); it sits at the far end via justify-between, which with no slot content
 * simply leaves the logo+label at the start — identical to the login layout.
 * The logo is shown full (no rounded crop), sized to align with the generic
 * outline IdP button.
 */
import { computed } from 'vue'
import { Button } from '@/components/ui/button'
import { providerBrand } from '@/lib/providerBrand'

const props = defineProps<{ protocol: string; label: string; disabled?: boolean }>()
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
    :disabled="disabled"
    :style="brandVars"
    class="w-full justify-between gap-2 border border-transparent bg-[var(--brand-bg)] text-[var(--brand-fg)] hover:bg-[var(--brand-hover-bg)] hover:text-[var(--brand-hover-fg)]"
    @click="$emit('click')"
  >
    <span class="flex min-w-0 items-center gap-2">
      <!-- 24px slot keeps icon+label aligned with the generic IdP button; the
           mark sits at 80% so it has margin on all sides (no edge-to-edge crop). -->
      <span class="inline-flex size-6 shrink-0 items-center justify-center">
        <img :src="brand.logo" alt="" aria-hidden="true" class="size-[80%] object-contain" />
      </span>
      <span class="truncate">{{ label }}</span>
    </span>
    <slot />
  </Button>
</template>
