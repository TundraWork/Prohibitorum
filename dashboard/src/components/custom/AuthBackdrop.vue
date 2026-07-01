<script setup lang="ts">
/**
 * AuthBackdrop — the full-bleed "Drenched" painted scenery behind the
 * unauthenticated threshold pages (login / consent / logout / error / enroll).
 *
 * Scoped exception (DESIGN.md §Threshold Exception): the authenticated app
 * stays pure white; only these centered auth surfaces carry a painted scene.
 * Legibility is non-negotiable — a contrast scrim layered over the scene
 * guarantees WCAG 2.2 AA for the near-opaque card and corner chrome above it.
 *
 * Scene source — three-tier precedence:
 *   1. Admin-uploaded background (branding store hasCustomBackground → backgroundSrc),
 *      served from /branding/background and cache-busted via the backgroundEtag.
 *   2. Build-time asset: drop a real painting at
 *      src/assets/auth-scene.{png,jpg,jpeg,webp,avif} and it is picked up
 *      automatically (resolved at build time below).
 *   3. If neither is present, a painterly CSS-gradient placeholder is used.
 *
 * Motion: the scene is static — there is no animation to honour for
 * prefers-reduced-motion, by construction.
 */

import { computed } from 'vue'
import { useBrandingStore } from '@/stores/branding'

const branding = useBrandingStore()

// Optional real scene asset resolved at build time (empty object → no match).
const sceneModules = import.meta.glob(
  '../../assets/auth-scene.{png,jpg,jpeg,webp,avif}',
  { eager: true, query: '?url', import: 'default' },
) as Record<string, string>
const assetUrl: string | undefined = Object.values(sceneModules)[0]

// Precedence: admin-uploaded DB background → build-time asset → CSS placeholder.
const sceneUrl = computed<string | undefined>(() =>
  branding.hasCustomBackground ? branding.backgroundSrc : assetUrl,
)
</script>

<template>
  <div class="auth-backdrop" aria-hidden="true">
    <!-- Scene: real painting if provided, else the painterly CSS placeholder. -->
    <div
      class="auth-backdrop__scene"
      :class="{ 'auth-backdrop__scene--placeholder': !sceneUrl }"
      :style="sceneUrl ? { backgroundImage: `url(${sceneUrl})` } : undefined"
    />
    <!-- Contrast scrim: a soft wash that mutes the scene and guarantees AA. -->
    <div class="auth-backdrop__scrim" />
  </div>
</template>

<style scoped>
.auth-backdrop {
  position: fixed;
  inset: 0;
  overflow: hidden;
}

.auth-backdrop__scene {
  position: absolute;
  inset: 0;
  background-size: cover;
  background-position: center;
}

/* Painterly placeholder — a "Drenched" dusk: a deep Tide-teal vault washing down
   into a warm Ember/Amber horizon glow, with a whisper of Sage in the midground.
   Confident colour here is the point (DESIGN.md §Threshold Exception): the card
   above is opaque, so it carries legibility; the scene supplies the warmth. */
.auth-backdrop__scene--placeholder {
  background:
    /* warm ember/amber dawn-glow blooming up from the lower centre */
    radial-gradient(135% 95% at 50% 128%, oklch(0.76 0.155 58 / 0.92), transparent 56%),
    /* a deeper ember pool, lower-left, for asymmetry and richness */
    radial-gradient(72% 64% at 20% 112%, oklch(0.68 0.150 38 / 0.60), transparent 60%),
    /* cool tide light raking in from the upper-right — the vault's calm */
    radial-gradient(95% 85% at 90% -8%, oklch(0.64 0.110 200 / 0.65), transparent 55%),
    /* deep tide base, top→foot, with a sage whisper before the warm horizon */
    linear-gradient(
      164deg,
      oklch(0.40 0.110 218) 0%,
      oklch(0.49 0.122 206) 40%,
      oklch(0.58 0.110 165 / 0.45) 68%,
      oklch(0.80 0.150 72 / 0.40) 100%
    );
}

/* Soft white bloom behind the centred card, fading to clear at the edges so the
   warm scene stays rich in the periphery. The card is opaque (legibility lives
   there) and the corner locale chip carries its own surface, so the scrim can be
   gentle — it only keeps the composition calm around the card. */
.auth-backdrop__scrim {
  position: absolute;
  inset: 0;
  background: radial-gradient(
    58% 52% at 50% 44%,
    oklch(1 0 0 / 0.62),
    oklch(1 0 0 / 0.22) 56%,
    oklch(1 0 0 / 0) 82%
  );
}
</style>
