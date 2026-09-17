<script setup lang="ts">
/** SecurityView (/security) — stacks the factor cards + the coarse revoke action. */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useResource } from '@/composables/useResource'
import { memberQuery } from '@/queries/resources'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { TriangleAlert } from 'lucide-vue-next'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription } from '@/components/ui/alert'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import PasskeysCard from '@/pages/security/PasskeysCard.vue'
import PasswordTotpCard from '@/pages/security/PasswordTotpCard.vue'
import RecoveryCodesCard from '@/pages/security/RecoveryCodesCard.vue'

interface MeFactors {
  passwordSet: boolean
  totpEnrolled: boolean
  recoveryCodesRemaining: number
  passkeyCount: number
}

const { t } = useI18n()
const { busy, run, error, clear } = useApi('credentials')
const confirmOpen = ref(false)
const { flag: done, trigger: triggerDone } = useTransientFlag()
const factorsQuery = useResource(memberQuery<MeFactors>('factors'))
const factors = computed(() => factorsQuery.data.value)
const factorsError = computed(() => !!factorsQuery.error.value)

async function revoke(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/auth/revoke-password-totp')
    return true as const
  }, t('sudo.reason.revokeFactors')))
  confirmOpen.value = false
  if (ok) triggerDone()
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('security.title') }}</h1>
    <Alert v-if="factorsError" role="alert">
      <AlertDescription>{{ t('security.factorsLoadError') }}</AlertDescription>
    </Alert>
    <PasskeysCard />
    <PasswordTotpCard :password-set="factors?.passwordSet" :totp-enrolled="factors?.totpEnrolled" />
    <RecoveryCodesCard :remaining="factors?.recoveryCodesRemaining" :totp-enabled="factors?.totpEnrolled" />

    <Card class="border-destructive/30 bg-destructive/[0.02]">
      <CardHeader>
        <CardTitle class="flex items-center gap-2 text-destructive">
          <TriangleAlert class="size-4 shrink-0" aria-hidden="true" />
          {{ t('security.revoke.title') }}
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-sm text-muted">{{ t('security.revoke.help') }}</p>
        <ErrorPanel :error="error" @dismiss="clear" />
        <StatusMessage :show="done">{{ t('security.revoke.done') }}</StatusMessage>
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
