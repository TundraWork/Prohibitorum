<script setup lang="ts">
/**
 * EntityIconUpload — admin icon upload/remove for an app or provider, modeled on
 * the instance-icon block in SettingsView. Shows the current icon (AppIcon) and
 * Upload / Remove buttons; both mutations go through withSudo. Emits `changed`
 * so the parent refetches (which re-supplies iconUrl).
 *
 * basePath: /api/prohibitorum/oidc-applications/<clientId> (etc.)
 */
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import AppIcon from '@/components/custom/AppIcon.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

const props = defineProps<{ basePath: string; name: string; iconUrl?: string | null }>()
const emit = defineEmits<{ changed: [] }>()

const { t } = useI18n()
const { busy, run, error, clear } = useApi()
const fileInput = ref<HTMLInputElement | null>(null)

async function onPick(e: Event): Promise<void> {
  const file = (e.target as HTMLInputElement).files?.[0]
  if (!file) return
  const ok = await run(() => withSudo(async () => {
    await api.upload(`${props.basePath}/icon`, file)
    return true as const
  }, t('sudo.reason.saveChanges')))
  if (fileInput.value) fileInput.value.value = ''
  if (ok) emit('changed')
}

async function remove(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.del(`${props.basePath}/icon`)
    return true as const
  }, t('sudo.reason.saveChanges')))
  if (ok) emit('changed')
}
</script>

<template>
  <Card>
    <CardHeader><CardTitle>{{ t('entityIcon.title') }}</CardTitle></CardHeader>
    <CardContent class="flex flex-col gap-3">
      <ErrorPanel :error="error" @dismiss="clear" />
      <div class="flex items-center gap-4">
        <span class="rounded-md ring-1 ring-inset ring-border">
          <AppIcon :src="iconUrl" :name="name" size="lg" />
        </span>
        <div class="flex flex-col gap-2">
          <p class="text-xs text-muted">{{ t('entityIcon.hint') }}</p>
          <div class="flex gap-2">
            <input ref="fileInput" type="file" accept="image/png,image/jpeg,image/webp" class="hidden" data-test="icon-input" @change="onPick" />
            <Button variant="outline" size="sm" :disabled="busy" data-test="icon-upload" @click="fileInput?.click()">{{ t('entityIcon.upload') }}</Button>
            <Button v-if="iconUrl" variant="outline" size="sm" :disabled="busy" data-test="icon-remove" @click="remove">{{ t('entityIcon.remove') }}</Button>
          </div>
        </div>
      </div>
    </CardContent>
  </Card>
</template>
