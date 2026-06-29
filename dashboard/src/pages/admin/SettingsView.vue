<script setup lang="ts">
/** SettingsView (/admin/settings) — edit instance name + icon + maintenance mode (admin + sudo). */
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

const { t } = useI18n()
const { busy, run, errorText } = useApi()
const branding = useBrandingStore()
const name = ref(branding.instanceName)
const { flag: savedFlag, trigger: triggerSaved } = useTransientFlag(2000)
const fileInput = ref<HTMLInputElement | null>(null)
const uploadError = ref('')

// Maintenance mode local state — initialised from the branding store on mount.
const maintenanceMode = ref(branding.maintenanceMode)
const maintenanceMessage = ref(branding.maintenanceMessage)
const { flag: maintenanceSavedFlag, trigger: triggerMaintenanceSaved } = useTransientFlag(2000)

onMounted(async () => {
  name.value = branding.instanceName
  await branding.ensureLoaded()
  maintenanceMode.value = branding.maintenanceMode
  maintenanceMessage.value = branding.maintenanceMessage
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
</script>

<template>
  <div class="flex max-w-xl flex-col gap-6">
    <div class="flex flex-col gap-1">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.settings.title') }}</h1>
      <p class="text-sm text-muted">{{ t('admin.settings.help') }}</p>
    </div>

    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
      <AlertDescription>{{ errorText }}</AlertDescription>
    </Alert>

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
      <CardHeader><CardTitle>{{ t('admin.settings.iconLabel') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-3">
        <div class="flex items-center gap-4">
          <span class="inline-flex size-12 items-center justify-center overflow-hidden rounded-md bg-ember/10 ring-1 ring-inset ring-border">
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
  </div>
</template>
