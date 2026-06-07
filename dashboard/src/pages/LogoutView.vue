<script setup lang="ts">
/**
 * LogoutView — the sign-out landing (/logout).
 *
 * On mount: POST /api/prohibitorum/auth/logout (idempotent, 204 even if already
 * signed out), then clear the auth store, then show a "signed out" landing with
 * a link back to /login.
 *
 * Documented limitation: the OIDC `post_logout_redirect_uri` "return to the
 * application" branch was unreachable in the previous build — the logout
 * endpoint only revokes the session and returns 204; it has no notion of an RP
 * to bounce back to. We deliberately do NOT invent that behavior here. If/when
 * the backend grows an RP-initiated logout flow (passing a validated
 * post_logout_redirect_uri + id_token_hint), this landing would branch on it;
 * until then there is only the local "signed out" terminal state.
 */
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import { Button } from '@/components/ui/button'

const { t } = useI18n()
const auth = useAuthStore()

onMounted(async () => {
  try {
    await api.post('/api/prohibitorum/auth/logout')
  } catch {
    // Already signed out / network hiccup — the landing is terminal either way.
  } finally {
    auth.clear()
  }
})
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-lg font-semibold tracking-tight text-ink">{{ t('logout.title') }}</h1>
    </template>

    <div class="flex flex-col items-center gap-6 text-center">
      <p class="text-sm text-muted">{{ t('logout.message') }}</p>
      <Button as-child class="w-full">
        <RouterLink to="/login">{{ t('logout.signInAgain') }}</RouterLink>
      </Button>
    </div>
  </CenteredLayout>
</template>
