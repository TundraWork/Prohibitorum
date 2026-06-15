<script setup lang="ts">
/**
 * DevicesView (/devices) — approve a new device by its pairing code.
 * lookup (not sudo) shows the initiator context; approve is sudo-gated; cancel
 * drops the pairing. The lookup → confirm → approve sequence IS the
 * confirmation, so there is no extra ConfirmDialog here.
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { relativeTime, formatDateTime } from '@/lib/time'
import { formatUserAgent } from '@/lib/userAgent'
import { MonitorSmartphone } from 'lucide-vue-next'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import CodeField from '@/components/custom/CodeField.vue'

interface Lookup {
  pairingId: string
  displayCode: string
  initiatorUa: string
  initiatorIp: string
  createdAt: string
  expiresAt: string
  alreadyBound: boolean
}

const { t } = useI18n()
const { busy, run, errorText } = useApi()

const code = ref('')
const found = ref<Lookup | null>(null)
const approved = ref(false)

async function lookup(): Promise<void> {
  approved.value = false
  const c = code.value.trim()
  if (!c) return
  const res = await run(() => api.get<Lookup>(
    `/api/prohibitorum/me/devices/pair/lookup?code=${encodeURIComponent(c)}`))
  if (res) found.value = res
}
async function approve(): Promise<void> {
  const c = code.value.trim()
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/me/devices/pair/approve', { code: c })
    return true as const
  }, t('sudo.reason.approveDevice')))
  if (ok) { approved.value = true; found.value = null; code.value = '' }
}
async function cancel(): Promise<void> {
  const c = code.value.trim()
  const ok = await run(async () => {
    await api.post('/api/prohibitorum/me/devices/pair/cancel', { code: c })
    return true as const
  })
  if (ok) { found.value = null; code.value = '' }
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('devices.title') }}</h1>
    <p class="text-sm text-muted">{{ t('devices.help') }}</p>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

    <p v-if="approved" class="text-sm text-sage" role="status">{{ t('devices.approved') }}</p>

    <!-- Entry -->
    <Card v-if="!found">
      <CardContent class="flex flex-col gap-3 py-4">
        <label class="text-sm font-medium text-ink" for="code">{{ t('devices.codeLabel') }}</label>
        <div class="flex items-center gap-2">
          <Input id="code" name="code" v-model="code" :placeholder="t('devices.codePlaceholder')"
                 autocomplete="off" class="font-mono uppercase" @keydown.enter.prevent="lookup" />
          <Button type="button" class="shrink-0" :disabled="busy || !code.trim()" :aria-busy="busy"
                  data-test="lookup" @click="lookup">
            {{ busy ? t('devices.lookingUp') : t('devices.lookup') }}
          </Button>
        </div>
      </CardContent>
    </Card>

    <!-- Confirmation -->
    <Card v-else>
      <CardHeader>
        <CardTitle class="flex items-center gap-2">
          <MonitorSmartphone class="size-4 shrink-0" aria-hidden="true" />
          {{ t('devices.confirmTitle') }}
        </CardTitle>
      </CardHeader>
      <CardContent class="flex flex-col gap-3 text-sm">
        <CodeField :value="found.displayCode" />
        <div class="flex min-w-0 flex-col gap-1">
          <span class="truncate text-ink font-medium" :title="found.initiatorUa">{{ formatUserAgent(found.initiatorUa) }}</span>
          <span class="truncate text-muted">{{ t('devices.ipAddress') }}: {{ found.initiatorIp }}</span>
          <span v-if="found.createdAt" class="truncate text-muted">{{ t('devices.started') }}: {{ relativeTime(found.createdAt) }}</span>
          <span v-if="found.expiresAt" class="truncate text-muted">{{ t('devices.expires') }}: {{ formatDateTime(found.expiresAt) }}</span>
        </div>
        <p v-if="found.alreadyBound" class="text-sm text-sage" role="status">{{ t('devices.alreadyBound') }}</p>
        <div class="flex gap-2">
          <Button v-if="!found.alreadyBound" type="button" :disabled="busy" data-test="approve" @click="approve">
            {{ t('devices.approve') }}
          </Button>
          <Button v-if="found.alreadyBound" type="button" variant="outline" :disabled="busy" data-test="done"
                  @click="found = null; code = ''">
            {{ t('devices.done') }}
          </Button>
          <Button v-if="!found.alreadyBound" type="button" variant="outline" :disabled="busy" data-test="cancel" @click="cancel">
            {{ t('devices.cancelPairing') }}
          </Button>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
