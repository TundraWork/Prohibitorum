<script setup lang="ts">
/** AdminForwardAuthAppsView (/admin/forward-auth-apps) — list of forward-auth services; inline create. */
import { nextTick, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { Plus, Waypoints, X } from 'lucide-vue-next'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import FormSection from '@/components/custom/FormSection.vue'
import EmptyState from '@/components/custom/EmptyState.vue'

interface ScopeEntry { name: string; description: string }
interface ScopeRow extends ScopeEntry { _id: number }

interface ForwardAuthApp {
  clientId: string
  displayName: string
  forwardAuthHost: string
  accessRestricted: boolean
  disabled: boolean
  createdAt: string
}

const { t } = useI18n()
const router = useRouter()
const { busy, run, errorText } = useApi()

const rows = ref<ForwardAuthApp[]>([])
const createOpen = ref(false)
const created = ref(false)

const clientId = ref('')
const host = ref('')
const displayName = ref('')

// Scope vocabulary for create form
let scopeUid = 0
const createScopeRows = ref<ScopeRow[]>([])
const createScopeListEl = ref<HTMLElement | null>(null)

function addCreateScope(): void {
  createScopeRows.value.push({ _id: scopeUid++, name: '', description: '' })
  nextTick(() => {
    const inputs = createScopeListEl.value?.querySelectorAll<HTMLInputElement>('input[data-scope-name]')
    inputs?.[inputs.length - 1]?.focus()
  })
}
function removeCreateScope(i: number): void { createScopeRows.value.splice(i, 1) }

async function load(): Promise<void> {
  const res = await run(() => api.get<ForwardAuthApp[]>('/api/prohibitorum/forward-auth-apps'))
  if (res) rows.value = res
}

function go(id: string): void { router.push(`/admin/forward-auth-apps/${id}`) }

function openCreate(): void {
  clientId.value = ''
  host.value = ''
  displayName.value = ''
  createScopeRows.value = []
  created.value = false
  createOpen.value = true
}

async function create(): Promise<void> {
  created.value = false
  const res = await run(() => withSudo(() => api.post<ForwardAuthApp>('/api/prohibitorum/forward-auth-apps', {
    clientId: clientId.value,
    host: host.value,
    displayName: displayName.value,
    scopes: createScopeRows.value.map(({ _id: _, ...e }) => e),
  })))
  if (res) {
    createOpen.value = false
    created.value = true
    await load()
  }
}

onMounted(load)
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.forwardAuth.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.forwardAuth.create') }}</Button>
    </div>
    <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite"><AlertDescription>{{ errorText }}</AlertDescription></Alert>
    <StatusMessage :show="created">{{ t('admin.forwardAuth.created') }}</StatusMessage>

    <Card v-if="createOpen">
      <CardHeader><CardTitle>{{ t('admin.forwardAuth.createTitle') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-4 py-4">
        <FormSection :title="t('admin.forwardAuth.configTitle')">
          <div class="flex flex-col gap-1.5">
            <Label for="clientId">{{ t('admin.forwardAuth.clientId') }}</Label>
            <Input id="clientId" name="clientId" v-model="clientId" autocomplete="off" />
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.clientIdDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="host">{{ t('admin.forwardAuth.host') }}</Label>
            <Input id="host" name="host" v-model="host" inputmode="url" :placeholder="t('admin.forwardAuth.hostPlaceholder')" />
            <p class="text-xs text-muted">{{ t('admin.forwardAuth.hostDesc') }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.forwardAuth.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" />
          </div>
        </FormSection>
        <FormSection :title="t('admin.forwardAuth.scopesLabel')">
          <div v-if="createScopeRows.length" ref="createScopeListEl" class="flex flex-col gap-3">
            <div class="hidden grid-cols-[1fr_2fr_2rem] gap-2 text-xs font-medium text-muted sm:grid">
              <span>{{ t('admin.forwardAuth.scopeName') }}</span>
              <span>{{ t('admin.forwardAuth.scopeDescription') }}</span>
              <span />
            </div>
            <div
              v-for="(row, i) in createScopeRows"
              :key="row._id"
              class="grid grid-cols-1 gap-2 rounded-md border border-border p-3 sm:grid-cols-[1fr_2fr_2rem] sm:items-start sm:gap-2 sm:rounded-none sm:border-0 sm:p-0"
              :data-test="`create-scope-row-${i}`"
            >
              <div class="flex flex-col gap-1">
                <span class="text-xs font-medium text-muted sm:hidden">{{ t('admin.forwardAuth.scopeName') }}</span>
                <Input
                  v-model="createScopeRows[i].name"
                  :placeholder="t('admin.forwardAuth.scopeName')"
                  :aria-label="t('admin.forwardAuth.scopeName')"
                  data-scope-name
                  :data-test="`create-scope-name-${i}`"
                />
              </div>
              <div class="flex flex-col gap-1">
                <span class="text-xs font-medium text-muted sm:hidden">{{ t('admin.forwardAuth.scopeDescription') }}</span>
                <Input
                  v-model="createScopeRows[i].description"
                  :placeholder="t('admin.forwardAuth.scopeDescription')"
                  :aria-label="t('admin.forwardAuth.scopeDescription')"
                  :data-test="`create-scope-desc-${i}`"
                />
              </div>
              <div class="flex justify-end sm:block">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  class="shrink-0 text-muted hover:text-ink sm:mt-0.5"
                  :aria-label="t('admin.forwardAuth.removeScope')"
                  :data-test="`create-scope-remove-${i}`"
                  @click="removeCreateScope(i)"
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
            data-test="create-scope-add"
            @click="addCreateScope"
          >
            <Plus class="size-4" aria-hidden="true" />
            {{ t('admin.forwardAuth.addScope') }}
          </Button>
        </FormSection>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.forwardAuth.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="busy && !rows.length" :rows="5" :cols="3" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.forwardAuth.colName') }} · {{ t('admin.forwardAuth.clientId') }}</TableHead>
          <TableHead>{{ t('admin.forwardAuth.colHost') }}</TableHead>
          <TableHead>{{ t('admin.forwardAuth.colState') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="c in rows" :key="c.clientId" class="cursor-pointer" tabindex="0"
                  :data-test="`fa-row-${c.clientId}`"
                  @click="go(c.clientId)" @keydown.enter="go(c.clientId)" @keydown.space.prevent="go(c.clientId)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ c.displayName }}</span>
              <span class="truncate text-muted">{{ c.clientId }}</span>
            </div>
          </TableCell>
          <TableCell><span class="truncate font-mono text-sm text-muted">{{ c.forwardAuthHost }}</span></TableCell>
          <TableCell>
            <StatusBadge :variant="c.disabled ? 'danger' : 'success'">
              {{ c.disabled ? t('admin.forwardAuth.disabled') : t('admin.forwardAuth.active') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!errorText && !createOpen" :icon="Waypoints" :title="t('admin.forwardAuth.empty')" />
  </div>
</template>
