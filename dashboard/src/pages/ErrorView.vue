<script setup lang="ts">
/**
 * ErrorView — the plain-language error landing (/error?error=…&error_description=…).
 *
 * Renders a human message from the query (PRODUCT.md: no jargon, no stack
 * traces, no codes shown to the user). Resolution order:
 *   1. errors.<error> i18n copy (the known backend / OIDC code)
 *   2. the raw error_description (only if it looks human — see below)
 *   3. a generic fallback
 *
 * `error` here can be a backend AuthError code, an OIDC error (e.g.
 * access_denied, server_error), or our own routed codes (e.g. forbidden from
 * the router guard). error_description is shown only as a fallback because
 * upstream descriptions are sometimes terse/technical.
 */
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import CenteredLayout from '@/pages/CenteredLayout.vue'
import { Button } from '@/components/ui/button'

const route = useRoute()
const { t, te } = useI18n()

const code = computed(() => String(route.query.error ?? ''))
const description = computed(() => {
  const d = route.query.error_description
  return typeof d === 'string' ? d : ''
})

const message = computed(() => {
  const key = `errors.${code.value}`
  if (code.value && te(key)) return t(key)
  if (description.value) return description.value
  return t('error.defaultMessage')
})
</script>

<template>
  <CenteredLayout>
    <template #title>
      <h1 class="text-xl font-semibold tracking-tight text-ink">{{ t('error.title') }}</h1>
    </template>

    <div class="flex flex-col items-center gap-6 text-center">
      <p role="alert" class="text-sm text-ink">{{ message }}</p>
      <Button as-child variant="outline" class="w-full">
        <RouterLink to="/login">{{ t('error.returnToLogin') }}</RouterLink>
      </Button>
    </div>
  </CenteredLayout>
</template>
