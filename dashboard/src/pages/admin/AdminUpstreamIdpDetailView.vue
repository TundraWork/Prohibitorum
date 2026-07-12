<script setup lang="ts">
/** AdminUpstreamIdpDetailView (/admin/identity-providers/:slug) — edit, rotate secret, delete. */
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useRoute, useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import RadioCardGroup from '@/components/custom/RadioCardGroup.vue'
import ScopeSelector from '@/components/custom/ScopeSelector.vue'
import ListInput from '@/components/custom/ListInput.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import { UPSTREAM_SCOPE_SUGGESTIONS } from '@/lib/scopes'
import SettingRow from '@/components/custom/SettingRow.vue'
import FormSection from '@/components/custom/FormSection.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'
import EntityIconUpload from '@/components/custom/EntityIconUpload.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import type { IdentityProvider } from './AdminUpstreamIdpsView.vue'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run, clear, errorText } = useApi()

const slug = String(route.params.slug)
const idp = ref<IdentityProvider | null>(null)
const notFound = ref(false)

const displayName = ref(''); const issuerUrl = ref(''); const clientId = ref('')
const mode = ref('auto_provision'); const scopes = ref<string[]>([]); const allowedDomains = ref<string[]>([])
const usernameClaim = ref(''); const displayNameClaim = ref(''); const emailClaim = ref(''); const pictureClaim = ref('')
const requireVerifiedEmail = ref(false); const disabled = ref(false)
const { flag: saved, trigger: triggerSaved } = useTransientFlag()

const isSteam = computed(() => idp.value?.protocol === 'steam')
const newSecret = ref(''); const { flag: rotated, trigger: triggerRotated } = useTransientFlag()
const confirmDelete = ref(false)

function validateDomain(s: string): string | null { return /^[a-z0-9.-]+\.[a-z]{2,}$/i.test(s) ? null : t('admin.upstream.domainInvalid') }

const upstreamScopesKnown = computed(() => UPSTREAM_SCOPE_SUGGESTIONS.map((s) => ({ value: s.value, description: t(s.descKey) })))

async function load(): Promise<void> {
  const i = await run(() => api.get<IdentityProvider>(`/api/prohibitorum/identity-providers/${slug}`))
  if (!i) { if (error.value?.code === 'upstream_idp_not_found') notFound.value = true; return }
  idp.value = i
  displayName.value = i.displayName; issuerUrl.value = i.issuerUrl; clientId.value = i.clientId
  mode.value = i.mode; scopes.value = [...i.scopes]; allowedDomains.value = [...i.allowedDomains]
  usernameClaim.value = i.usernameClaim; displayNameClaim.value = i.displayNameClaim; emailClaim.value = i.emailClaim; pictureClaim.value = i.pictureClaim
  requireVerifiedEmail.value = i.requireVerifiedEmail; disabled.value = i.disabled
}

async function save(): Promise<void> {
  const updated = await run(() => withSudo(() => api.put<IdentityProvider>(`/api/prohibitorum/identity-providers/${slug}`, {
    displayName: displayName.value, issuerUrl: issuerUrl.value, clientId: clientId.value, mode: mode.value,
    scopes: scopes.value, allowedDomains: allowedDomains.value, usernameClaim: usernameClaim.value,
    displayNameClaim: displayNameClaim.value, emailClaim: emailClaim.value, pictureClaim: pictureClaim.value,
    requireVerifiedEmail: requireVerifiedEmail.value, disabled: disabled.value,
  }), t('sudo.reason.saveChanges')))
  if (updated) { idp.value = updated; triggerSaved() }
}

async function rotate(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/identity-providers/rotate-secret', { slug, clientSecret: newSecret.value })
    return true as const
  }, t('sudo.reason.rotateSecret')))
  if (ok) { triggerRotated(); newSecret.value = '' }
}

// Flip the disabled flag on its own (independent of the config Save), via the
// dedicated set-disabled endpoint.
async function toggleDisabled(): Promise<void> {
  const next = !disabled.value
  const updated = await run(() => withSudo(() =>
    api.post<IdentityProvider>('/api/prohibitorum/identity-providers/set-disabled', { slug, disabled: next }),
    t('sudo.reason.disableApp')))
  if (updated) { idp.value = updated; disabled.value = updated.disabled }
}

async function destroy(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/identity-providers/delete', { slug })
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDelete.value = false
  if (ok) router.push('/admin/identity-providers')
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/identity-providers" :label="t('admin.upstream.back')" />
    <ErrorPanel v-if="error && !notFound" :error="error" @dismiss="clear" />
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.upstream.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !idp" />

    <template v-else-if="idp">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ idp.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.upstream.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-5">
          <FormSection :title="t('admin.upstream.sectionConnection')">
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.slug') }}</Label>
              <p class="font-mono text-sm text-muted" data-test="idp-slug">{{ idp.slug }}</p>
              <p class="text-xs text-muted">{{ t('admin.upstream.slugDesc') }}</p>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.protocol') }}</Label>
              <p class="font-mono text-sm text-muted" data-test="idp-protocol">{{ idp.protocol ?? 'oidc' }}</p>
            </div>
            <div class="flex flex-col gap-1.5">
              <Label for="displayName">{{ t('admin.upstream.displayName') }}</Label>
              <Input id="displayName" name="displayName" v-model="displayName" autocomplete="off" />
            </div>
            <template v-if="!isSteam">
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
                <Label for="scopes">{{ t('admin.upstream.scopes') }}</Label>
                <ScopeSelector :known="upstreamScopesKnown" :allow-custom="true" v-model="scopes" />
                <p class="text-xs text-muted">{{ t('admin.upstream.scopesDesc') }}</p>
              </div>
            </template>
          </FormSection>
          <FormSection :title="t('admin.upstream.sectionProvisioning')">
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.mode') }}</Label>
              <RadioCardGroup v-model="mode" :aria-label="t('admin.upstream.mode')" :options="[
                {value:'auto_provision',title:t('admin.upstream.modeAutoProvision'),description:t('admin.upstream.modeAutoProvisionDesc')},
                {value:'invite_only',title:t('admin.upstream.modeInviteOnly'),description:t('admin.upstream.modeInviteOnlyDesc')},
                {value:'link_only',title:t('admin.upstream.modeLinkOnly'),description:t('admin.upstream.modeLinkOnlyDesc')}]" />
            </div>
            <div class="flex flex-col gap-1.5">
              <Label>{{ t('admin.upstream.allowedDomains') }}</Label>
              <ListInput v-model="allowedDomains" name="allowedDomains"
                :add-label="t('admin.upstream.addDomain')" :placeholder="t('admin.upstream.domainPlaceholder')" :validate="validateDomain" />
              <p class="text-xs text-muted">{{ t('admin.upstream.domainsHint') }}</p>
            </div>
            <SettingRow v-if="!isSteam" :label="t('admin.upstream.requireVerifiedEmail')" :description="t('admin.upstream.requireVerifiedEmailDesc')" for="requireVerifiedEmail">
              <Switch id="requireVerifiedEmail" v-model="requireVerifiedEmail" data-test="requireVerifiedEmail" />
            </SettingRow>
          </FormSection>
          <FormSection v-if="!isSteam" :title="t('admin.upstream.sectionClaims')">
            <div class="grid grid-cols-[minmax(7rem,auto)_1fr] items-center gap-x-3 gap-y-2">
              <Label class="text-sm" for="usernameClaim">{{ t('admin.upstream.usernameClaim') }}</Label>
              <Input id="usernameClaim" name="usernameClaim" class="h-8" v-model="usernameClaim" placeholder="preferred_username" autocomplete="off" data-test="claim-username" />
              <Label class="text-sm" for="displayNameClaim">{{ t('admin.upstream.displayNameClaim') }}</Label>
              <Input id="displayNameClaim" name="displayNameClaim" class="h-8" v-model="displayNameClaim" placeholder="name" autocomplete="off" data-test="claim-displayName" />
              <Label class="text-sm" for="emailClaim">{{ t('admin.upstream.emailClaim') }}</Label>
              <Input id="emailClaim" name="emailClaim" class="h-8" v-model="emailClaim" placeholder="email" autocomplete="off" data-test="claim-email" />
              <Label class="text-sm" for="pictureClaim">{{ t('admin.upstream.pictureClaim') }}</Label>
              <Input id="pictureClaim" name="pictureClaim" class="h-8" v-model="pictureClaim" placeholder="picture" autocomplete="off" data-test="claim-avatar" />
            </div>
            <p class="text-xs text-muted">{{ t('admin.upstream.claimsHint') }}</p>
          </FormSection>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.upstream.save') }}</Button>
            <StatusMessage :show="saved">{{ t('admin.upstream.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <EntityIconUpload
        :base-path="`/api/prohibitorum/identity-providers/${slug}`"
        :name="idp?.displayName ?? slug"
        :icon-url="idp?.iconUrl"
        @changed="load"
      />

      <!-- Danger zone: sensitive operations (disable, rotate secret, delete) grouped together. -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.upstream.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <SectionTitle as="h3">{{ t('admin.upstream.statusLabel') }}</SectionTitle>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.upstream.disabled') : t('admin.upstream.active') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.upstream.disabledDesc') }}</p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.upstream.enable') : t('admin.upstream.disable') }}
            </Button>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ isSteam ? t('admin.upstream.rotateTitleSteam') : t('admin.upstream.rotateTitle') }}</SectionTitle>
            <p class="text-xs text-muted">{{ isSteam ? t('admin.upstream.rotateBodySteam') : t('admin.upstream.rotateBody') }}</p>
            <div class="flex flex-col gap-1.5">
              <Label for="newSecret">{{ isSteam ? t('admin.upstream.steamApiKey') : t('admin.upstream.clientSecret') }}</Label>
              <Input id="newSecret" name="newSecret" type="password" v-model="newSecret" autocomplete="off" />
            </div>
            <StatusMessage :show="rotated">{{ t('admin.upstream.rotated') }}</StatusMessage>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy || !newSecret" data-test="rotate" @click="rotate">{{ isSteam ? t('admin.upstream.rotateConfirmSteam') : t('admin.upstream.rotateConfirm') }}</Button>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ t('admin.upstream.deleteTitle') }}</SectionTitle>
            <p class="text-xs text-muted">{{ t('admin.upstream.deleteHelp') }}</p>
            <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.upstream.delete') }}</Button>
          </div>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmDelete" :title="t('admin.upstream.deleteConfirmTitle')" :confirm-label="t('admin.upstream.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.upstream.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
