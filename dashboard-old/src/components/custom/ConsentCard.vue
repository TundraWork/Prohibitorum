<script setup lang="ts">
/**
 * ConsentCard — shared shell for the OIDC + SAML consent screens.
 *
 * Three stacked blocks, split by a rule: (1) the app requesting access (optional
 * logo + the heading slot), (2) the signed-in account with its avatar, (3) the
 * per-screen body + actions. The avatar resolves image → initials → generic icon
 * via UserAvatar, so an account with no uploaded picture still reads as "you".
 */
import { useI18n } from 'vue-i18n'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import { Separator } from '@/components/ui/separator'

defineProps<{
  logoUri?: string
  displayName: string
  accountName: string
  accountAvatarUrl?: string | null
  accountUsername?: string | null
  policyUri?: string
  tosUri?: string
}>()
const { t } = useI18n()
</script>

<template>
  <div class="flex flex-col gap-5">
    <!-- Block 1 — the app requesting access. -->
    <div class="flex flex-col items-center gap-3 text-center">
      <img
        v-if="logoUri"
        :src="logoUri"
        :alt="displayName"
        class="size-12 rounded-xl object-contain ring-1 ring-border"
      />
      <slot name="heading" />
    </div>

    <!-- Block 2 — who is signed in (with avatar). -->
    <div class="flex items-center gap-3 rounded-xl border border-border bg-sunken px-3.5 py-3">
      <UserAvatar
        :src="accountAvatarUrl"
        :display-name="accountName"
        :username="accountUsername"
        size="md"
      />
      <div class="min-w-0">
        <p class="text-xs text-muted">{{ t('consent.signedInAs') }}</p>
        <p class="truncate text-sm font-medium text-ink">{{ accountName }}</p>
      </div>
    </div>

    <Separator />

    <!-- Block 3 — per-screen permissions / notice (body) + actions. -->
    <slot name="body" />
    <slot name="actions" />

    <p v-if="policyUri || tosUri" class="text-center text-xs text-muted">
      <a v-if="policyUri" :href="policyUri" target="_blank" rel="noopener noreferrer" class="underline-offset-4 hover:underline">{{ t('consent.privacyPolicy') }}</a>
      <span v-if="policyUri && tosUri"> &middot; </span>
      <a v-if="tosUri" :href="tosUri" target="_blank" rel="noopener noreferrer" class="underline-offset-4 hover:underline">{{ t('consent.termsOfService') }}</a>
    </p>
  </div>
</template>
