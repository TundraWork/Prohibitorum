<script setup lang="ts">
/** SecurityView (/security) — stacks the factor cards + the coarse revoke action. */
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { TriangleAlert } from 'lucide-vue-next'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import PasskeysCard from '@/pages/security/PasskeysCard.vue'
import PasswordCard from '@/pages/security/PasswordCard.vue'
import TotpCard from '@/pages/security/TotpCard.vue'
import RecoveryCodesCard from '@/pages/security/RecoveryCodesCard.vue'

interface MeFactors {
  passwordSet: boolean
  totpEnrolled: boolean
  recoveryCodesRemaining: number
  passkeyCount: number
}

const { t } = useI18n()
const { busy, run, errorText } = useApi()
const confirmOpen = ref(false)
const { flag: done, trigger: triggerDone } = useTransientFlag()
const factors = ref<MeFactors | null>(null)

async function loadFactors(): Promise<void> {
  try {
    factors.value = await api.get<MeFactors>('/api/prohibitorum/me/factors')
  } catch {
    // non-fatal: badges simply won't render
  }
}

onMounted(loadFactors)

async function revoke(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/auth/revoke-password-totp')
    return true as const
  }))
  confirmOpen.value = false
  if (ok) { triggerDone(); await loadFactors() }
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('security.title') }}</h1>
    <PasskeysCard />
    <PasswordCard :set="factors?.passwordSet" @changed="loadFactors" />
    <TotpCard :enrolled="factors?.totpEnrolled" @changed="loadFactors" />
    <RecoveryCodesCard :remaining="factors?.recoveryCodesRemaining" @changed="loadFactors" />

    <Card class="border-destructive/30 bg-destructive/[0.02]">
      <CardHeader>
        <CardTitle class="flex items-center gap-2 text-destructive">
          <TriangleAlert class="size-4 shrink-0" aria-hidden="true" />
          {{ t('security.revoke.title') }}
        </CardTitle>
      </CardHeader>
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
