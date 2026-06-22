<script setup lang="ts">
/**
 * AppTile — one launchable app on the "My apps" home, as near-square "cover art".
 *
 * Layout: a large cover bleeds to three edges (top/left/right) — the uploaded
 * logo (object-cover) or a deterministic duotone gradient (`.app-cover`) carrying
 * a big white monogram. A slim footer holds the name. Two flush corner chips ride
 * the cover: the protocol glyph (top-left, tooltip) and the actions menu
 * (top-right, hover/focus-revealed). The whole tile launches via a stretched
 * overlay anchor that owns the focus ring; the chips sit above it at z-[2] so
 * they stay interactive without nesting controls inside the link.
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { appTintHue, srgbToOklch } from '@/lib/appColor'
import ProtocolBadge from '@/components/custom/ProtocolBadge.vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import { MoreVertical, ArrowUpRight, Copy, SlidersHorizontal, Trash2, Check } from 'lucide-vue-next'

export interface LaunchpadApp { kind: 'oidc' | 'forward_auth' | 'saml'; id: string; name: string; iconUrl?: string | null; launchUrl: string; accentColor?: string | null }
export interface ConsentInfo { scopes: string[] }

const props = withDefaults(defineProps<{ app: LaunchpadApp; consent?: ConsentInfo | null; isAdmin?: boolean }>(), {
  consent: null,
  isAdmin: false,
})
const emit = defineEmits<{
  (e: 'revoke', app: LaunchpadApp): void
  (e: 'copy', app: LaunchpadApp): void
  (e: 'manage', app: LaunchpadApp): void
}>()
const { t } = useI18n()

const hasConsent = computed(() => !!props.consent)
const canRevoke = computed(() => props.app.kind === 'oidc' && hasConsent.value)
const monogram = computed(() => (props.app.name.trim()[0] ?? '?').toUpperCase())

// Backdrop tint: derive hue + a calm chroma from the icon's server-extracted
// accent colour when present (a blue logo → faint blue, a grayscale logo →
// near-neutral); otherwise fall back to a deterministic hue from the name (used
// by both the backdrop and the generated monogram gradient).
const coverStyle = computed<Record<string, string>>(() => {
  const oklch = props.app.accentColor ? srgbToOklch(props.app.accentColor) : null
  if (oklch) {
    return { '--app-h': oklch.h.toFixed(1), '--app-c': Math.min(0.03, oklch.c).toFixed(4) }
  }
  return { '--app-h': String(appTintHue(props.app.name)), '--app-c': '0.018' }
})
const menuOpen = ref(false)

// A broken/deleted logo URL must fall back to the monogram cover, never a blank
// tile. Reset the flag if the source changes.
const imgFailed = ref(false)
watch(() => props.app.iconUrl, () => { imgFailed.value = false })
const showImg = computed(() => !!props.app.iconUrl && !imgFailed.value)
</script>

<template>
  <div
    class="group relative flex flex-col overflow-hidden rounded-2xl border border-line bg-card transition-all duration-200 ease-out hover:-translate-y-1 hover:shadow-lg"
  >
    <!-- Cover: a soft tinted backdrop with a large CENTERED icon. Real logos are
         contained (never cropped; transparency reads against the calm tint);
         logo-less apps get a generated gradient monogram icon. -->
    <div class="app-bg relative aspect-[4/3] w-full overflow-hidden" :style="coverStyle">
      <div class="absolute inset-0 flex items-center justify-center p-5">
        <div class="aspect-square h-full max-w-full transition-transform duration-300 ease-out group-hover:scale-105 motion-reduce:transition-none motion-reduce:group-hover:scale-100">
          <img v-if="showImg" :src="app.iconUrl!" alt="" class="size-full object-contain" loading="lazy" @error="imgFailed = true" />
          <div v-else class="app-cover flex size-full items-center justify-center rounded-2xl">
            <span
              aria-hidden="true"
              class="select-none text-[2.5rem] font-bold leading-none text-white/90 [text-shadow:0_1px_8px_oklch(0_0_0/0.30)]"
            >{{ monogram }}</span>
          </div>
        </div>
      </div>
    </div>

    <!-- Footer: name, a hover launch hint (off the icon), and an access mark. -->
    <div class="relative flex items-center gap-1.5 border-t border-line bg-card px-3 py-2.5">
      <span class="min-w-0 flex-1 truncate text-sm font-medium text-ink">{{ app.name }}</span>
      <ArrowUpRight
        class="size-4 shrink-0 text-muted opacity-0 transition-opacity duration-150 group-hover:opacity-100"
        aria-hidden="true"
      />
      <span v-if="hasConsent" :data-test="`consent-${app.id}`" class="shrink-0 text-sage-600" :title="t('myApps.accessGranted')">
        <Check class="size-4" aria-hidden="true" />
        <span class="sr-only">{{ t('myApps.accessGranted') }}</span>
      </span>
    </div>

    <!-- Stretched launch overlay: covers the whole tile, owns the focus ring. -->
    <a
      :href="app.launchUrl"
      target="_blank"
      rel="noopener noreferrer"
      :aria-label="t('myApps.open', { name: app.name })"
      :data-test="`launch-${app.id}`"
      class="absolute inset-0 z-[1] rounded-[inherit] outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-canvas"
    />

    <!-- Protocol glyph — flush top-left corner, description on hover. -->
    <ProtocolBadge
      :kind="app.kind"
      class="absolute left-1.5 top-1.5 z-[2] size-7 rounded-full bg-card/85 text-muted ring-1 ring-line backdrop-blur-sm hover:text-ink"
    />

    <!-- Actions menu — flush top-right corner; revealed on hover/focus, pinned open. -->
    <div
      class="absolute right-1.5 top-1.5 z-[2] transition-opacity"
      :class="menuOpen ? 'opacity-100' : 'opacity-0 group-hover:opacity-100 group-focus-within:opacity-100'"
    >
      <DropdownMenu v-model:open="menuOpen">
        <DropdownMenuTrigger as-child>
          <Button
            size="icon"
            class="size-7 rounded-full bg-card/85 text-muted ring-1 ring-line backdrop-blur-sm hover:bg-card hover:text-ink"
            :aria-label="t('myApps.menu')"
            :data-test="`menu-${app.id}`"
          >
            <MoreVertical class="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end" :side-offset="6">
          <DropdownMenuItem :data-test="`copy-${app.id}`" @select="emit('copy', app)">
            <Copy />
            <span>{{ t('myApps.copyLink') }}</span>
          </DropdownMenuItem>
          <DropdownMenuItem v-if="isAdmin" :data-test="`manage-${app.id}`" @select="emit('manage', app)">
            <SlidersHorizontal />
            <span>{{ t('myApps.manage') }}</span>
          </DropdownMenuItem>
          <template v-if="canRevoke">
            <DropdownMenuSeparator />
            <DropdownMenuItem
              :data-test="`revoke-${app.id}`"
              class="text-rose-600 focus:text-rose-700 data-[highlighted]:text-rose-700"
              @select="emit('revoke', app)"
            >
              <Trash2 />
              <span>{{ t('myApps.revoke') }}</span>
            </DropdownMenuItem>
          </template>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  </div>
</template>
