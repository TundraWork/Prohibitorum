<script setup lang="ts">
/** SettingsView (/admin/settings) — edit instance name + icon + maintenance mode (admin + sudo). */
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { useBrandingStore } from '@/stores/branding'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Select, SelectTrigger, SelectContent, SelectItem, SelectValue } from '@/components/ui/select'

const { t } = useI18n()
const { busy, run, error, clear, errorText } = useApi()
const branding = useBrandingStore()
const name = ref(branding.instanceName)
const { flag: savedFlag, trigger: triggerSaved } = useTransientFlag(2000)
const fileInput = ref<HTMLInputElement | null>(null)
const uploadError = ref('')
const bgInput = ref<HTMLInputElement | null>(null)
const bgError = ref('')

// Maintenance mode local state — initialised from the branding store on mount.
const maintenanceMode = ref(branding.maintenanceMode)
const maintenanceMessage = ref(branding.maintenanceMessage)
const { flag: maintenanceSavedFlag, trigger: triggerMaintenanceSaved } = useTransientFlag(2000)

// Client IP / Proxy local state — loaded from the API on mount.
const clientIpStrategy = ref<'direct' | 'forwarded' | 'header'>('direct')
const clientIpHeader = ref('CF-Connecting-IP')
const clientIpTrusted = ref('') // textarea, one CIDR per line
const { flag: clientIpSavedFlag, trigger: triggerClientIpSaved } = useTransientFlag(2000)

onMounted(async () => {
  name.value = branding.instanceName
  await branding.ensureLoaded()
  maintenanceMode.value = branding.maintenanceMode
  maintenanceMessage.value = branding.maintenanceMessage
  await loadClientIp()
})

async function saveName(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.put('/api/prohibitorum/admin/settings', { instanceName: name.value })
    return true as const
  }))
  if (ok) {
    await branding.load()
    triggerSaved()
  }
}

async function saveMaintenance(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.put('/api/prohibitorum/admin/settings/maintenance', {
      maintenanceMode: maintenanceMode.value,
      maintenanceMessage: maintenanceMessage.value,
    })
    return true as const
  }))
  if (ok) {
    await branding.load()
    triggerMaintenanceSaved()
  }
}

async function loadClientIp(): Promise<void> {
  const cfg = await run(() =>
    api.get<{ strategy: string; header: string; trustedProxies: string[] }>(
      '/api/prohibitorum/admin/settings/client-ip',
    ),
  )
  if (!cfg) return
  clientIpStrategy.value = (cfg.strategy as 'direct' | 'forwarded' | 'header') || 'direct'
  if (cfg.header) clientIpHeader.value = cfg.header
  clientIpTrusted.value = (cfg.trustedProxies || []).join('\n')
}

async function saveClientIp(): Promise<void> {
  const trustedProxies = clientIpTrusted.value
    .split('\n')
    .map((s) => s.trim())
    .filter((s) => s.length > 0)
  const ok = await run(() =>
    withSudo(async () => {
      await api.put('/api/prohibitorum/admin/settings/client-ip', {
        strategy: clientIpStrategy.value,
        header: clientIpStrategy.value === 'header' ? clientIpHeader.value : '',
        trustedProxies,
      })
      return true as const
    }),
  )
  if (ok) triggerClientIpSaved()
}

async function onPickFile(e: Event): Promise<void> {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  uploadError.value = ''
  const ok = await run(() => withSudo(async () => {
    await api.upload('/api/prohibitorum/admin/settings/icon', file)
    return true as const
  }))
  if (ok) {
    await branding.load()
  } else {
    uploadError.value = t('admin.settings.uploadError')
  }
  if (fileInput.value) fileInput.value.value = ''
}

async function removeIcon(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.del('/api/prohibitorum/admin/settings/icon')
    return true as const
  }))
  if (ok) await branding.load()
}

async function onPickBackground(e: Event): Promise<void> {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  bgError.value = ''
  const ok = await run(() => withSudo(async () => {
    await api.upload('/api/prohibitorum/admin/settings/background', file)
    return true as const
  }))
  if (ok) {
    await branding.load()
  } else {
    bgError.value = t('admin.settings.backgroundError')
  }
  if (bgInput.value) bgInput.value.value = ''
}

async function removeBackground(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.del('/api/prohibitorum/admin/settings/background')
    return true as const
  }))
  if (ok) await branding.load()
}
</script>

<template>
  <div class="flex max-w-xl flex-col gap-6">
    <div class="flex flex-col gap-1">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.settings.title') }}</h1>
      <p class="text-sm text-muted">{{ t('admin.settings.help') }}</p>
    </div>

    <ErrorPanel :error="error" @dismiss="clear" :is-admin="true" />

    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.nameLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex flex-col gap-1.5">
          <Label for="instance-name">{{ t('admin.settings.nameLabel') }}</Label>
          <Input id="instance-name" v-model="name" :disabled="busy" :maxlength="64" />
          <p class="text-xs text-muted">{{ t('admin.settings.nameHint') }}</p>
        </div>
        <div class="flex items-center gap-3">
          <Button :disabled="busy" data-test="save-name" @click="saveName">{{ t('admin.settings.save') }}</Button>
          <span v-if="savedFlag" class="text-sm text-muted" role="status">{{ t('admin.settings.saved') }}</span>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.maintenanceLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-xs text-muted">{{ t('admin.settings.maintenanceDescription') }}</p>
        <div class="flex items-center gap-3">
          <Switch id="maintenance-mode" v-model="maintenanceMode" :disabled="busy" :aria-label="t('admin.settings.maintenanceToggle')" />
          <Label for="maintenance-mode">{{ t('admin.settings.maintenanceToggle') }}</Label>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="maintenance-message">{{ t('admin.settings.maintenanceMessageLabel') }}</Label>
          <Textarea
            id="maintenance-message"
            v-model="maintenanceMessage"
            :disabled="busy"
            :placeholder="t('admin.settings.maintenanceMessagePlaceholder')"
            :maxlength="500"
            rows="3"
          />
        </div>
        <p class="text-xs text-amber-700 dark:text-amber-400">{{ t('admin.settings.maintenanceWarning') }}</p>
        <div class="flex items-center gap-3">
          <Button :disabled="busy" data-test="save-maintenance" @click="saveMaintenance">{{ t('admin.settings.maintenanceSave') }}</Button>
          <span v-if="maintenanceSavedFlag" class="text-sm text-muted" role="status">{{ t('admin.settings.saved') }}</span>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.clientIpLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <p class="text-xs text-muted">{{ t('admin.settings.clientIpHelp') }}</p>
        <div class="flex flex-col gap-1.5">
          <Label for="client-ip-strategy">{{ t('admin.settings.clientIpStrategyLabel') }}</Label>
          <Select :model-value="clientIpStrategy" @update:model-value="(v) => (clientIpStrategy = v as 'direct' | 'forwarded' | 'header')">
            <SelectTrigger id="client-ip-strategy" data-test="client-ip-strategy" :aria-label="t('admin.settings.clientIpStrategyLabel')">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="direct">{{ t('admin.settings.clientIpStrategyDirect') }}</SelectItem>
              <SelectItem value="forwarded">{{ t('admin.settings.clientIpStrategyForwarded') }}</SelectItem>
              <SelectItem value="header">{{ t('admin.settings.clientIpStrategyHeader') }}</SelectItem>
            </SelectContent>
          </Select>
        </div>
        <div v-if="clientIpStrategy === 'header'" class="flex flex-col gap-1.5">
          <Label for="client-ip-header">{{ t('admin.settings.clientIpHeaderLabel') }}</Label>
          <Input id="client-ip-header" v-model="clientIpHeader" :disabled="busy" data-test="client-ip-header" />
          <p class="text-xs text-muted">{{ t('admin.settings.clientIpHeaderHint') }}</p>
        </div>
        <div v-if="clientIpStrategy !== 'direct'" class="flex flex-col gap-1.5">
          <Label for="client-ip-trusted">{{ t('admin.settings.clientIpTrustedLabel') }}</Label>
          <Textarea id="client-ip-trusted" v-model="clientIpTrusted" :disabled="busy" rows="4" data-test="client-ip-trusted" />
          <p class="text-xs text-muted">{{ t('admin.settings.clientIpTrustedHint') }}</p>
          <p v-if="clientIpTrusted.trim() === ''" class="text-xs text-amber-700 dark:text-amber-400" data-test="client-ip-warning">
            {{ t('admin.settings.clientIpEmptyWarning') }}
          </p>
        </div>
        <div class="flex items-center gap-3">
          <Button :disabled="busy" data-test="save-client-ip" @click="saveClientIp">{{ t('admin.settings.clientIpSave') }}</Button>
          <span v-if="clientIpSavedFlag" class="text-sm text-muted" role="status">{{ t('admin.settings.saved') }}</span>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.iconLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex items-center gap-4">
          <span class="inline-flex size-12 shrink-0 items-center justify-center overflow-hidden rounded-md bg-ember/10 ring-1 ring-inset ring-border">
            <img :src="branding.iconSrc" :alt="branding.instanceName" class="size-full object-cover" />
          </span>
          <div class="flex flex-col gap-2">
            <p class="text-xs text-muted">{{ t('admin.settings.iconHint') }}</p>
            <div class="flex gap-2">
              <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp" class="hidden" data-test="icon-input" @change="onPickFile" />
              <Button variant="outline" size="sm" :disabled="busy" data-test="upload-icon" @click="fileInput?.click()">{{ t('admin.settings.upload') }}</Button>
              <Button v-if="branding.hasCustomIcon" variant="outline" size="sm" :disabled="busy" data-test="remove-icon" @click="removeIcon">{{ t('admin.settings.remove') }}</Button>
            </div>
            <p v-if="uploadError" class="text-sm text-destructive" role="alert">{{ uploadError }}</p>
          </div>
        </div>
      </CardContent>
    </Card>

    <Card>
      <CardHeader><CardTitle>{{ t('admin.settings.backgroundLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex items-start gap-4">
          <span class="inline-flex h-16 w-28 shrink-0 items-center justify-center overflow-hidden rounded-md bg-ember/10 ring-1 ring-inset ring-border">
            <img v-if="branding.hasCustomBackground" :src="branding.backgroundSrc" :alt="t('admin.settings.backgroundLabel')" class="size-full object-cover" />
          </span>
          <div class="flex flex-col gap-2">
            <p class="text-xs text-muted">{{ t('admin.settings.backgroundHint') }}</p>
            <div class="flex gap-2">
              <input ref="bgInput" type="file" accept="image/png,image/jpeg,image/webp" class="hidden" data-test="background-input" @change="onPickBackground" />
              <Button variant="outline" size="sm" :disabled="busy" data-test="upload-background" @click="bgInput?.click()">{{ t('admin.settings.backgroundUpload') }}</Button>
              <Button v-if="branding.hasCustomBackground" variant="outline" size="sm" :disabled="busy" data-test="remove-background" @click="removeBackground">{{ t('admin.settings.backgroundRemove') }}</Button>
            </div>
            <p v-if="bgError" class="text-sm text-destructive" role="alert">{{ bgError }}</p>
          </div>
        </div>
      </CardContent>
    </Card>
  </div>
</template>
