<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import RadioCardGroup from '@/components/custom/RadioCardGroup.vue'
import type { OIDCConnectionConfig } from '@/lib/oidcProviderConfig'
const model = defineModel<OIDCConnectionConfig>({ required: true })
const { t } = useI18n()
const endpoints = ['authorization', 'token', 'userinfo', 'jwks'] as const
function setMode(value: string): void {
  model.value = { ...model.value, configurationMode: value as OIDCConnectionConfig['configurationMode'] }
}
function setAuth(value: string): void {
  model.value = { ...model.value, tokenAuthMethod: value as OIDCConnectionConfig['tokenAuthMethod'], pkceMethod: value === 'none' ? 'S256' : model.value.pkceMethod }
}
function setEndpoint(key: typeof endpoints[number], value: string | number): void {
  model.value = { ...model.value, endpoints: { ...model.value.endpoints, [key]: value === '' ? null : String(value) } }
}
</script>
<template>
  <div class="flex min-w-0 flex-col gap-4" data-test="oidc-connection">
    <div class="flex flex-col gap-1.5">
      <Label>{{ t('admin.upstream.configurationMode') }}</Label>
      <RadioCardGroup :model-value="model.configurationMode" @update:model-value="setMode" :aria-label="t('admin.upstream.configurationMode')" :options="[
        { value: 'discovery', title: t('admin.upstream.discoveryMode'), description: t('admin.upstream.discoveryModeDesc') },
        { value: 'manual', title: t('admin.upstream.manualMode'), description: t('admin.upstream.manualModeDesc') },
      ]" />
    </div>
    <div v-for="key in endpoints" :key="key" class="flex min-w-0 flex-col gap-1.5">
      <Label :for="`endpoint-${key}`">{{ t(`admin.upstream.endpoint${key}`) }}</Label>
      <Input :id="`endpoint-${key}`" :name="`endpoint-${key}`" :model-value="model.endpoints[key] ?? ''" @update:model-value="setEndpoint(key, $event)" autocomplete="off" />
      <p class="text-xs text-muted">{{ t(model.configurationMode === 'discovery' ? 'admin.upstream.endpointDiscoveryHint' : key === 'userinfo' ? 'admin.upstream.endpointUserinfoHint' : 'admin.upstream.endpointRequired') }}</p>
    </div>
    <div class="flex flex-col gap-1.5">
      <Label>{{ t('admin.upstream.tokenAuthMethod') }}</Label>
      <RadioCardGroup :model-value="model.tokenAuthMethod" @update:model-value="setAuth" :aria-label="t('admin.upstream.tokenAuthMethod')" :options="[
        ...(model.configurationMode === 'discovery' ? [{ value: 'discovery', title: t('admin.upstream.authDiscovery') }] : []),
        { value: 'client_secret_basic', title: 'client_secret_basic' },
        { value: 'client_secret_post', title: 'client_secret_post' },
        { value: 'none', title: t('admin.upstream.authNone') },
      ]" />
    </div>
    <div class="flex flex-col gap-1.5">
      <Label>{{ t('admin.upstream.pkceMethod') }}</Label>
      <RadioCardGroup v-if="model.tokenAuthMethod !== 'none'" v-model="model.pkceMethod" :aria-label="t('admin.upstream.pkceMethod')" :options="[
        { value: 'S256', title: 'S256' }, { value: 'plain', title: 'plain' }, { value: 'off', title: t('admin.upstream.pkceOff') },
      ]" />
      <p v-else class="text-sm text-muted">{{ t('admin.upstream.publicPKCE') }}</p>
    </div>
  </div>
</template>
