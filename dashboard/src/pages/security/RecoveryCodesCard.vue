<script setup lang="ts">
/**
 * RecoveryCodesCard — regenerate recovery codes (sudo-gated; needs confirmed TOTP).
 * When totpEnabled is false the Regenerate button is replaced with a hint so the
 * user cannot click into a server-side bad_request dead-end.
 *
 * Uses ErrorPanel for all API errors. The contextual need-TOTP hint is shown
 * as separate inline guidance when the error code is bad_request (this
 * endpoint's bad_request specifically means "no TOTP enrolled"), alongside the
 * ErrorPanel which retains the requestId/details/dismiss behavior.
 */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import RecoveryCodesDisplay from '@/components/custom/RecoveryCodesDisplay.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'

const props = withDefaults(defineProps<{
  remaining?: number
  /** Whether a TOTP authenticator is enrolled. When explicitly false, the
   *  Regenerate button is hidden — the server requires TOTP before issuing
   *  recovery codes. undefined = factors not yet loaded (show button).
   *  withDefaults preserves undefined to avoid Vue boolean-prop coercion. */
  totpEnabled?: boolean
}>(), { totpEnabled: undefined })
const emit = defineEmits<{ (e: 'changed'): void }>()

const { t } = useI18n()
const { busy, error, run, clear } = useApi()
const codes = ref<string[]>([])

// Contextual hint: this endpoint's bad_request means "no TOTP enrolled".
// Shown as separate inline guidance alongside ErrorPanel (which handles
// the requestId/details/dismiss). Other error codes render ErrorPanel only.
const showNeedTotpHint = computed(() => error.value?.code === 'bad_request')

async function regenerate(): Promise<void> {
  const r = await run(() => withSudo(() =>
    api.post<{ recovery_codes: string[] }>('/api/prohibitorum/me/recovery-codes/regenerate'),
    t('sudo.reason.regenerateCodes')))
  if (r) { codes.value = r.recovery_codes ?? []; emit('changed') }
}
</script>

<template>
  <Card>
    <CardHeader class="flex flex-row items-center gap-2">
      <CardTitle>{{ t('security.recovery.title') }}</CardTitle>
      <StatusBadge
        v-if="props.remaining !== undefined"
        :variant="props.remaining > 4 ? 'success' : props.remaining > 0 ? 'caution' : 'danger'"
      >
        {{ t('security.factors.recoveryRemaining', { n: props.remaining }) }}
      </StatusBadge>
    </CardHeader>
    <CardContent class="flex flex-col gap-4">
      <template v-if="codes.length > 0">
        <RecoveryCodesDisplay :codes="codes" />
      </template>
      <template v-else>
        <p class="text-sm text-muted">{{ t('security.recovery.help') }}</p>
        <!-- Guard: server rejects regeneration without a confirmed TOTP; show a
             non-clickable hint instead of an enabled button that leads to an error. -->
        <p
          v-if="props.totpEnabled === false"
          class="text-sm text-muted"
          data-test="recovery-no-totp-hint"
        >{{ t('security.recovery.needTotp') }}</p>
        <Button
          v-if="props.totpEnabled !== false"
          type="button"
          variant="outline"
          class="w-fit"
          :disabled="busy"
          @click="regenerate"
        >
          {{ t('security.recovery.regenerate') }}
        </Button>
      </template>
      <!-- Contextual need-TOTP hint shown when bad_request means "no TOTP" -->
      <p v-if="showNeedTotpHint" class="text-sm text-muted">{{ t('security.recovery.needTotp') }}</p>
      <ErrorPanel :error="error" @dismiss="clear" />
    </CardContent>
  </Card>
</template>
