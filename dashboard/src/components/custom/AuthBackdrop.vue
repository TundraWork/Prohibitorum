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
 * Scene source — zero-code swap:
 *   Drop a real painting at  src/assets/auth-scene.{png,jpg,jpeg,webp,avif}
 *   and it is picked up automatically (resolved at build time below). If no
 *   such file exists, a painterly CSS-gradient placeholder is used.
 *
 * Motion: the scene is static — there is no animation to honour for
 * prefers-reduced-motion, by construction.
 */

// Optional real scene asset. import.meta.glob resolves matching files at build
// time; with no match it is an empty object → we fall back to the CSS placeholder.
const sceneModules = import.meta.glob(
  '../../assets/auth-scene.{png,jpg,jpeg,webp,avif}',
  { eager: true, query: '?url', import: 'default' },
) as Record<string, string>
const sceneUrl: string | undefined = Object.values(sceneModules)[0]
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

/* Painterly placeholder — a "Drenched" dawn: a deep Tide sky washing down into
   a warm Ember/Amber horizon. Muted on purpose; the scrim + card carry the
   legibility, the painting only supplies warmth. */
.auth-backdrop__scene--placeholder {
  background:
    radial-gradient(120% 85% at 50% 118%, oklch(0.70 0.150 42 / 0.55), transparent 60%),
    radial-gradient(90% 70% at 82% 8%, oklch(0.55 0.118 205 / 0.40), transparent 55%),
    linear-gradient(
      162deg,
      oklch(0.47 0.130 205) 0%,
      oklch(0.55 0.118 205) 46%,
      oklch(0.76 0.140 75 / 0.55) 100%
    );
}

/* Soft white wash, strongest where the card sits, fading outward. Keeps a hint
   of the painted warmth at the edges while guaranteeing the card region and any
   corner chrome hold WCAG 2.2 AA over any image. */
.auth-backdrop__scrim {
  position: absolute;
  inset: 0;
  background: radial-gradient(
    72% 62% at 50% 46%,
    oklch(1 0 0 / 0.80),
    oklch(1 0 0 / 0.58) 62%,
    oklch(1 0 0 / 0.46) 100%
  );
}
</style>
