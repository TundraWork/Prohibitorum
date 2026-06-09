<script setup lang="ts">
/** AdminUpstreamIdpDetailView (/admin/identity-providers/:slug) — edit, rotate secret, delete. */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Switch } from '@/components/ui/switch'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import RadioCardGroup from '@/components/custom/RadioCardGroup.vue'
import TagInput from '@/components/custom/TagInput.vue'
import ListInput from '@/components/custom/ListInput.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import type { IdentityProvider } from './AdminUpstreamIdpsView.vue'

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run } = useApi()

const slug = String(route.params.slug)
const idp = ref<IdentityProvider | null>(null)
const notFound = ref(false)

const displayName = ref(''); const issuerUrl = ref(''); const clientId = ref('')
const mode = ref('auto_provision'); const scopes = ref<string[]>([]); const allowedDomains = ref<string[]>([])
const usernameClaim = ref(''); const displayNameClaim = ref(''); const emailClaim = ref('')
const requireVerifiedEmail = ref(false); const disabled = ref(false)
const saved = ref(false)

const newSecret = ref(''); const rotated = ref(false)
const confirmDelete = ref(false)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function validateDomain(s: string): string | null { return /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(s) ? null : t('admin.upstream.domainInvalid') }

async function load(): Promise<void> {
  const i = await run(() => api.get<IdentityProvider>(`/api/prohibitorum/identity-providers/${slug}`))
  if (!i) { if (error.value?.code === 'upstream_idp_not_found') notFound.value = true; return }
  idp.value = i
  displayName.value = i.displayName; issuerUrl.value = i.issuerUrl; clientId.value = i.clientId
  mode.value = i.mode; scopes.value = [...i.scopes]; allowedDomains.value = [...i.allowedDomains]
  usernameClaim.value = i.usernameClaim; displayNameClaim.value = i.displayNameClaim; emailClaim.value = i.emailClaim
  requireVerifiedEmail.value = i.requireVerifiedEmail; disabled.value = i.disabled
}

async function save(): Promise<void> {
  saved.value = false; rotated.value = false
  const updated = await run(() => withSudo(() => api.put<IdentityProvider>(`/api/prohibitorum/identity-providers/${slug}`, {
    displayName: displayName.value, issuerUrl: issuerUrl.value, clientId: clientId.value, mode: mode.value,
    scopes: scopes.value, allowedDomains: allowedDomains.value, usernameClaim: usernameClaim.value,
    displayNameClaim: displayNameClaim.value, emailClaim: emailClaim.value,
    requireVerifiedEmail: requireVerifiedEmail.value, disabled: disabled.value,
  })))
  if (updated) { idp.value = updated; saved.value = true }
}

async function rotate(): Promise<void> {
  saved.value = false; rotated.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/identity-providers/rotate-secret', { slug, clientSecret: newSecret.value })
    return true as const
  }))
  if (ok) { rotated.value = true; newSecret.value = '' }
}

async function destroy(): Promise<void> {
  saved.value = false; rotated.value = false
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/identity-providers/delete', { slug })
    return true as const
  }))
  confirmDelete.value = false
  if (ok) router.push('/admin/identity-providers')
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <RouterLink to="/admin/identity-providers" class="text-sm text-muted underline-offset-4 hover:underline">{{ t('admin.upstream.back') }}</RouterLink>
    <Alert v-if="errorText && !notFound" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.upstream.notFound') }}</p>

    <template v-else-if="idp">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ idp.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.upstream.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex max-w-xl flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.upstream.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="issuerUrl">{{ t('admin.upstream.issuerUrl') }}</Label>
            <Input id="issuerUrl" name="issuerUrl" v-model="issuerUrl" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.upstream.issuerUrlDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="clientId">{{ t('admin.upstream.clientId') }}</Label>
            <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.upstream.mode') }}</Label>
            <RadioCardGroup v-model="mode" :aria-label="t('admin.upstream.mode')" :options="[
              {value:'auto_provision',title:t('admin.upstream.modeAutoProvision'),description:t('admin.upstream.modeAutoProvisionDesc')},
              {value:'invite_only',title:t('admin.upstream.modeInviteOnly'),description:t('admin.upstream.modeInviteOnlyDesc')},
              {value:'link_only',title:t('admin.upstream.modeLinkOnly'),description:t('admin.upstream.modeLinkOnlyDesc')}]" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="scopes">{{ t('admin.upstream.scopes') }}</Label>
            <TagInput input-id="scopes" v-model="scopes" :placeholder="t('admin.upstream.scopesHint')" :aria-label="t('admin.upstream.scopes')" />
            <p class="text-xs text-muted">{{ t('admin.upstream.scopesDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.upstream.allowedDomains') }}</Label>
            <ListInput v-model="allowedDomains" name="allowedDomains"
              :add-label="t('admin.upstream.addDomain')" :placeholder="t('admin.upstream.domainPlaceholder')" :validate="validateDomain" />
            <p class="text-xs text-muted">{{ t('admin.upstream.domainsHint') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="usernameClaim">{{ t('admin.upstream.usernameClaim') }}</Label>
            <Input id="usernameClaim" name="usernameClaim" v-model="usernameClaim" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.upstream.usernameClaimDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayNameClaim">{{ t('admin.upstream.displayNameClaim') }}</Label>
            <Input id="displayNameClaim" name="displayNameClaim" v-model="displayNameClaim" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.upstream.displayNameClaimDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="emailClaim">{{ t('admin.upstream.emailClaim') }}</Label>
            <Input id="emailClaim" name="emailClaim" v-model="emailClaim" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.upstream.emailClaimDesc') }}</p>
          </div>
          <SettingRow :label="t('admin.upstream.requireVerifiedEmail')" :description="t('admin.upstream.requireVerifiedEmailDesc')" for="requireVerifiedEmail">
            <Switch id="requireVerifiedEmail" v-model="requireVerifiedEmail" data-test="requireVerifiedEmail" />
          </SettingRow>
          <SettingRow :label="t('admin.upstream.disabled')" :description="t('admin.upstream.disabledDesc')" for="disabled">
            <Switch id="disabled" v-model="disabled" data-test="disabled" />
          </SettingRow>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.upstream.save') }}</Button>
            <span v-if="saved" class="text-sm text-sage" role="status">{{ t('admin.upstream.saved') }}</span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.upstream.rotateTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-sm text-muted">{{ t('admin.upstream.rotateBody') }}</p>
          <div class="flex flex-col gap-1.5">
            <Label for="newSecret">{{ t('admin.upstream.clientSecret') }}</Label>
            <Input id="newSecret" name="newSecret" type="password" v-model="newSecret" autocomplete="off" />
          </div>
          <span v-if="rotated" class="text-sm text-sage" role="status">{{ t('admin.upstream.rotated') }}</span>
          <Button type="button" variant="outline" class="w-fit" :disabled="busy || !newSecret" data-test="rotate" @click="rotate">{{ t('admin.upstream.rotateConfirm') }}</Button>
        </CardContent>
      </Card>

      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.upstream.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <p class="text-sm text-muted">{{ t('admin.upstream.deleteHelp') }}</p>
          <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.upstream.delete') }}</Button>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmDelete" :title="t('admin.upstream.deleteConfirmTitle')" :confirm-label="t('admin.upstream.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.upstream.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
