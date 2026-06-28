<script setup lang="ts">
/**
 * AdminForwardAuthAppDetailView (/admin/forward-auth-apps/:clientId) —
 * edit display-name + host, show the host-substituted Traefik snippet, reuse
 * the OIDC AppAccessCard for RBAC, and a danger zone (disable/enable + delete).
 * No rotate-secret — forward-auth clients are public.
 */
import { computed, nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { TriangleAlert, Plus, X } from 'lucide-vue-next'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardHeader, CardTitle, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert'
import { Separator } from '@/components/ui/separator'
import ConfirmDialog from '@/components/custom/ConfirmDialog.vue'
import SectionTitle from '@/components/custom/SectionTitle.vue'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import BackLink from '@/components/custom/BackLink.vue'
import CodeBlock from '@/components/custom/CodeBlock.vue'
import AppAccessCard from '@/components/custom/AppAccessCard.vue'
import EntityIconUpload from '@/components/custom/EntityIconUpload.vue'

interface ScopeEntry { name: string; description: string }
interface ScopeRow extends ScopeEntry { _id: number }

interface ForwardAuthApp {
  clientId: string
  displayName: string
  forwardAuthHost: string
  accessRestricted: boolean
  disabled: boolean
  createdAt: string
  iconUrl?: string | null
  scopes: ScopeEntry[]
}

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const { busy, error, run, errorText } = useApi()

const clientId = String(route.params.clientId)
const app = ref<ForwardAuthApp | null>(null)
const notFound = ref(false)

const displayName = ref('')
const host = ref('')
const disabled = ref(false)
const { flag: saved, trigger: triggerSaved } = useTransientFlag()
const confirmDelete = ref(false)

// Scope vocabulary editor
let scopeUid = 0
const scopeRows = ref<ScopeRow[]>([])
const scopeListEl = ref<HTMLElement | null>(null)

function toScopeRows(entries: ScopeEntry[]): ScopeRow[] {
  return entries.map((e) => ({ ...e, _id: scopeUid++ }))
}
function addScope(): void {
  scopeRows.value.push({ _id: scopeUid++, name: '', description: '' })
  nextTick(() => {
    const inputs = scopeListEl.value?.querySelectorAll<HTMLInputElement>('input[data-scope-name]')
    inputs?.[inputs.length - 1]?.focus()
  })
}
function removeScope(i: number): void { scopeRows.value.splice(i, 1) }

const traefikSnippet = computed(() => {
  const origin = window.location.origin
  const h = host.value || app.value?.forwardAuthHost || 'app.example.com'
  return `http:
  middlewares:
    prohibitorum-forward-auth:
      forwardAuth:
        address: "${origin}/api/prohibitorum/forward-auth/verify"
        trustForwardHeader: true
        authResponseHeaders:
          - Remote-User
          - Remote-Name
          - Remote-Email
          - Remote-Groups
  routers:
    # Your protected app (define "app-svc" to point at your backend):
    protected-app:
      rule: "Host(\`${h}\`)"
      service: app-svc
      middlewares:
        - prohibitorum-forward-auth
    # The fixed forward-auth prefix → Prohibitorum (define "prohibitorum-svc"):
    prohibitorum-forward-auth:
      rule: "Host(\`${h}\`) && PathPrefix(\`/.prohibitorum-forward-auth\`)"
      service: prohibitorum-svc`
})

async function load(): Promise<void> {
  const c = await run(() => api.get<ForwardAuthApp>(`/api/prohibitorum/forward-auth-apps/${clientId}`))
  if (!c) { if (error.value?.code === 'client_not_found') notFound.value = true; return }
  app.value = c
  displayName.value = c.displayName
  host.value = c.forwardAuthHost
  disabled.value = c.disabled
  scopeRows.value = toScopeRows(c.scopes ?? [])
}

async function save(): Promise<void> {
  const updated = await run(() => withSudo(() => api.put<ForwardAuthApp>(`/api/prohibitorum/forward-auth-apps/${clientId}`, {
    displayName: displayName.value,
    host: host.value,
    scopes: scopeRows.value.map(({ _id: _, ...e }) => e),
  }), t('sudo.reason.saveChanges')))
  if (updated) { app.value = updated; triggerSaved() }
}

async function toggleDisabled(): Promise<void> {
  const next = !disabled.value
  const updated = await run(() => withSudo(() =>
    api.post<ForwardAuthApp>('/api/prohibitorum/forward-auth-apps/set-disabled', { clientId, disabled: next }),
    t('sudo.reason.disableApp')))
  if (updated) { app.value = updated; disabled.value = updated.disabled }
}

async function destroy(): Promise<void> {
  const ok = await run(() => withSudo(async () => {
    await api.post('/api/prohibitorum/forward-auth-apps/delete', { clientId })
    return true as const
  }, t('sudo.reason.deleteApp')))
  confirmDelete.value = false
  if (ok) router.push('/admin/forward-auth-apps')
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-2xl flex-col gap-6">
    <BackLink to="/admin/forward-auth-apps" :label="t('admin.forwardAuth.back')" />
    <Alert v-if="errorText && !notFound" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('admin.forwardAuth.notFound') }}</p>

    <CardSkeleton v-else-if="busy && !app" />

    <template v-else-if="app">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ app.displayName }}</h1>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.forwardAuth.configTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Label>{{ t('admin.forwardAuth.clientId') }}</Label>
            <p class="font-mono text-sm text-muted" data-test="fa-client-id">{{ app.clientId }}</p>
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.clientIdDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.forwardAuth.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="host">{{ t('admin.forwardAuth.host') }}</Label>
            <Input id="host" name="host" v-model="host" inputmode="url" :placeholder="t('admin.forwardAuth.hostPlaceholder')" />
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.hostDesc') }}</p>
          </div>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save" @click="save">{{ t('admin.forwardAuth.save') }}</Button>
            <StatusMessage :show="saved">{{ t('admin.forwardAuth.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <!-- Scope vocabulary editor -->
      <Card>
        <CardHeader><CardTitle>{{ t('admin.forwardAuth.scopesLabel') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-3">
            <div v-if="scopeRows.length" ref="scopeListEl" class="flex flex-col gap-3">
              <div class="hidden grid-cols-[1fr_2fr_2rem] gap-2 text-xs font-medium text-muted sm:grid">
                <span>{{ t('admin.forwardAuth.scopeName') }}</span>
                <span>{{ t('admin.forwardAuth.scopeDescription') }}</span>
                <span />
              </div>
              <div
                v-for="(row, i) in scopeRows"
                :key="row._id"
                class="grid grid-cols-1 gap-2 rounded-md border border-border p-3 sm:grid-cols-[1fr_2fr_2rem] sm:items-start sm:gap-2 sm:rounded-none sm:border-0 sm:p-0"
                :data-test="`scope-row-${i}`"
              >
                <div class="flex flex-col gap-1">
                  <span class="text-xs font-medium text-muted sm:hidden">{{ t('admin.forwardAuth.scopeName') }}</span>
                  <Input
                    v-model="scopeRows[i].name"
                    :placeholder="t('admin.forwardAuth.scopeName')"
                    :aria-label="t('admin.forwardAuth.scopeName')"
                    data-scope-name
                    :data-test="`scope-name-${i}`"
                  />
                </div>
                <div class="flex flex-col gap-1">
                  <span class="text-xs font-medium text-muted sm:hidden">{{ t('admin.forwardAuth.scopeDescription') }}</span>
                  <Input
                    v-model="scopeRows[i].description"
                    :placeholder="t('admin.forwardAuth.scopeDescription')"
                    :aria-label="t('admin.forwardAuth.scopeDescription')"
                    :data-test="`scope-desc-${i}`"
                  />
                </div>
                <div class="flex justify-end sm:block">
                  <Button
                    type="button"
                    variant="ghost"
                    size="icon-sm"
                    class="shrink-0 text-muted hover:text-ink sm:mt-0.5"
                    :aria-label="t('admin.forwardAuth.removeScope')"
                    :data-test="`scope-remove-${i}`"
                    @click="removeScope(i)"
                  >
                    <X class="size-4" aria-hidden="true" />
                  </Button>
                </div>
              </div>
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              class="w-fit"
              data-test="scope-add"
              @click="addScope"
            >
              <Plus class="size-4" aria-hidden="true" />
              {{ t('admin.forwardAuth.addScope') }}
            </Button>
          </div>
          <div class="flex items-center gap-3">
            <Button type="button" :disabled="busy" data-test="save-scopes" @click="save">{{ t('admin.forwardAuth.save') }}</Button>
            <StatusMessage :show="saved">{{ t('admin.forwardAuth.saved') }}</StatusMessage>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>{{ t('admin.forwardAuth.traefikTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-3">
          <Alert class="border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-800/50 dark:bg-amber-950/30 dark:text-amber-200">
            <TriangleAlert class="size-4 text-amber-600 dark:text-amber-400" aria-hidden="true" />
            <AlertTitle>{{ t('admin.forwardAuth.trustTitle') }}</AlertTitle>
            <AlertDescription>
              <ul class="mt-1 list-disc pl-4 space-y-1">
                <li>{{ t('admin.forwardAuth.trustIsolation') }}</li>
                <li>{{ t('admin.forwardAuth.trustHeaders') }}</li>
                <li>{{ t('admin.forwardAuth.trustStripAuth') }}</li>
              </ul>
            </AlertDescription>
          </Alert>
          <p class="text-xs text-muted">{{ t('admin.forwardAuth.traefikDesc') }}</p>
          <CodeBlock :value="traefikSnippet" />
        </CardContent>
      </Card>

      <EntityIconUpload
        :base-path="`/api/prohibitorum/oidc-applications/${clientId}`"
        :name="app?.displayName ?? clientId"
        :icon-url="app?.iconUrl"
        @changed="load"
      />

      <AppAccessCard kind="oidc" :app-id="clientId" />

      <!-- Danger zone (kept LAST). No rotate-secret — FA clients are public. -->
      <Card class="border-destructive/30 bg-destructive/[0.02]">
        <CardHeader><CardTitle class="text-destructive">{{ t('admin.forwardAuth.dangerTitle') }}</CardTitle></CardHeader>
        <CardContent class="flex flex-col gap-4">
          <div class="flex flex-col gap-2">
            <div class="flex items-center gap-2">
              <SectionTitle as="h3">{{ t('admin.forwardAuth.statusLabel') }}</SectionTitle>
              <StatusBadge :variant="disabled ? 'danger' : 'success'" data-test="status-badge">
                {{ disabled ? t('admin.forwardAuth.disabled') : t('admin.forwardAuth.active') }}
              </StatusBadge>
            </div>
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.disabledDesc') }}</p>
            <Button type="button" variant="outline" class="w-fit" :disabled="busy" data-test="disable-toggle" @click="toggleDisabled">
              {{ disabled ? t('admin.forwardAuth.enable') : t('admin.forwardAuth.disable') }}
            </Button>
          </div>

          <Separator />
          <div class="flex flex-col gap-2">
            <SectionTitle as="h3">{{ t('admin.forwardAuth.deleteTitle') }}</SectionTitle>
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.deleteHelp') }}</p>
            <Button type="button" variant="destructive" class="w-fit" :disabled="busy" data-test="delete" @click="confirmDelete = true">{{ t('admin.forwardAuth.delete') }}</Button>
          </div>
        </CardContent>
      </Card>
    </template>

    <ConfirmDialog :open="confirmDelete" :title="t('admin.forwardAuth.deleteConfirmTitle')" :confirm-label="t('admin.forwardAuth.delete')" :busy="busy"
      @update:open="(v) => { if (!v) confirmDelete = false }" @cancel="confirmDelete = false" @confirm="destroy">
      {{ t('admin.forwardAuth.deleteConfirmBody') }}
    </ConfirmDialog>
  </div>
</template>
