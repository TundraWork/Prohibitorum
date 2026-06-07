<script setup lang="ts">
/** SecurityView (/security) — stacks the factor cards + the coarse revoke action. */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import PasskeysCard from '@/pages/security/PasskeysCard.vue'
import PasswordCard from '@/pages/security/PasswordCard.vue'
import TotpCard from '@/pages/security/TotpCard.vue'
import RecoveryCodesCard from '@/pages/security/RecoveryCodesCard.vue'

const { t, te } = useI18n()
const { busy, error, run } = useApi()
const confirmOpen = ref(false)
const done = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

async function revoke(): Promise<void> {
  done.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/auth/revoke-password-totp')
    return true as const
  }))
  confirmOpen.value = false
  if (ok) done.value = true
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('security.title') }}</h1>
    <PasskeysCard />
    <PasswordCard />
    <TotpCard />
    <RecoveryCodesCard />

    <Card>
      <CardHeader><CardTitle>{{ t('security.revoke.title') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-sm text-muted">{{ t('security.revoke.help') }}</p>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
        <p v-if="done" class="text-sm text-sage" role="status">{{ t('security.revoke.done') }}</p>
        <Button type="button" variant="destructive" class="w-fit" :disabled="busy" @click="confirmOpen = true">
          {{ t('security.revoke.button') }}
        </Button>
      </CardContent>
    </Card>

    <ConfirmDialog
      :open="confirmOpen"
      :title="t('security.revoke.confirmTitle')"
      :confirm-label="t('security.revoke.button')"
      :busy="busy"
      @update:open="(v) => { if (!v) confirmOpen = false }"
      @cancel="confirmOpen = false"
      @confirm="revoke"
    >
      {{ t('security.revoke.confirmBody') }}
    </ConfirmDialog>
  </div>
</template>
