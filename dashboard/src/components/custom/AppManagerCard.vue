<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { withSudo } from '@/lib/sudo'
import { buildPagePath, type Page } from '@/lib/pagination'
import { formatDateTime } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import FormSection from '@/components/custom/FormSection.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'

type AppKind = 'oidc' | 'forward_auth' | 'saml'

interface AppManagerView {
  id: number
  username: string
  displayName: string
  disabled: boolean
  assignedAt: string
}

interface AccountView {
  id: number
  username: string
  displayName: string
  role: string
  disabled: boolean
  avatarUrl?: string | null
}

const props = defineProps<{
  kind: AppKind
  appId: string
}>()

const MANAGER_COLLECTIONS: Record<AppKind, string> = {
  oidc: 'oidc-applications',
  forward_auth: 'forward-auth-apps',
  saml: 'saml-applications',
}

const { locale, t } = useI18n()
const managersApi = useApi()
const accountsApi = useApi()
const mutationApi = useApi()

const managers = ref<AppManagerView[]>([])
const accountResults = ref<AccountView[]>([])
const searchQuery = ref('')
const hasSearched = ref(false)
const searchInputId = `app-manager-search-${useId()}`

const managerEndpoint = computed(() =>
  `/api/prohibitorum/${MANAGER_COLLECTIONS[props.kind]}/${encodeURIComponent(props.appId)}/managers`,
)

const assignedManagerIds = computed(() => new Set(managers.value.map((manager) => manager.id)))
const availableAccounts = computed(() =>
  accountResults.value.filter(
    (account) => account.role === 'app_manager' && !assignedManagerIds.value.has(account.id),
  ),
)
const displayedManagerError = computed(() => mutationApi.error.value ?? managersApi.error.value)
const initialManagersLoading = computed(() => managersApi.busy.value && managers.value.length === 0)

let active = true
let identityVersion = 0
let accountRequestVersion = 0
let managerLoadRequested = 0
let managerLoadCompleted = 0
let managerLoadPromise: Promise<void> | null = null

function identityName(account: Pick<AccountView, 'displayName' | 'username'>): string {
  return account.displayName || account.username
}

async function drainManagerLoads(): Promise<void> {
  while (active && managerLoadCompleted < managerLoadRequested) {
    const request = managerLoadRequested
    const identity = identityVersion
    const endpoint = managerEndpoint.value
    const result = await managersApi.run(() => api.get<AppManagerView[]>(endpoint))
    managerLoadCompleted = request

    if (!active) return
    if (identity !== identityVersion || request !== managerLoadRequested) {
      if (identity !== identityVersion) managersApi.clear()
      continue
    }
    if (result !== undefined) managers.value = result
  }
}

function loadManagers(): Promise<void> {
  managerLoadRequested += 1
  if (managerLoadPromise === null) {
    managerLoadPromise = drainManagerLoads().finally(() => {
      managerLoadPromise = null
    })
  }
  return managerLoadPromise
}

async function searchAccounts(): Promise<void> {
  if (accountsApi.busy.value) return

  const query = searchQuery.value.trim()
  const request = ++accountRequestVersion
  const identity = identityVersion
  accountResults.value = []
  hasSearched.value = query !== ''

  if (!query) {
    accountsApi.clear()
    return
  }

  const result = await accountsApi.run(() =>
    api.get<Page<AccountView>>(buildPagePath('/api/prohibitorum/accounts', { q: query })),
  )

  if (!active || identity !== identityVersion || request !== accountRequestVersion) {
    if (identity !== identityVersion) accountsApi.clear()
    return
  }
  if (result !== undefined) accountResults.value = result.items ?? []
}

async function mutateManager(path: string, accountId: number): Promise<void> {
  if (mutationApi.busy.value) return

  const identity = identityVersion
  const previousError = mutationApi.error.value
  let callbackRan = false

  await mutationApi.run(() =>
    withSudo(() => {
      callbackRan = true
      return api.post<void>(path, { accountId })
    }, t('sudo.reason.saveChanges')),
  )

  if (!active || identity !== identityVersion) {
    mutationApi.clear()
    return
  }
  if (!callbackRan) {
    mutationApi.error.value = previousError
    return
  }
  if (mutationApi.error.value !== null) return

  await loadManagers()
}

function assignManager(account: AccountView): Promise<void> | undefined {
  if (account.disabled || assignedManagerIds.value.has(account.id)) return
  return mutateManager(managerEndpoint.value, account.id)
}

function removeManager(manager: AppManagerView): Promise<void> {
  return mutateManager(`${managerEndpoint.value}/remove`, manager.id)
}

function clearManagerError(): void {
  mutationApi.clear()
  managersApi.clear()
}

watch(
  [() => props.kind, () => props.appId],
  () => {
    identityVersion += 1
    accountRequestVersion += 1
    managers.value = []
    accountResults.value = []
    searchQuery.value = ''
    hasSearched.value = false
    managersApi.clear()
    accountsApi.clear()
    mutationApi.clear()
    void loadManagers()
  },
  { immediate: true },
)

onBeforeUnmount(() => {
  active = false
  identityVersion += 1
  accountRequestVersion += 1
})
</script>

<template>
  <Card>
    <CardHeader>
      <CardTitle>{{ t('admin.appManagers.title') }}</CardTitle>
      <CardDescription>{{ t('admin.appManagers.description') }}</CardDescription>
    </CardHeader>

    <CardContent class="flex flex-col gap-6">
      <ErrorPanel
        :error="displayedManagerError"
        :is-admin="true"
        @dismiss="clearManagerError"
      />

      <FormSection :title="t('admin.appManagers.assigned')">
        <EmptyState
          v-if="initialManagersLoading"
          :title="t('admin.appManagers.loading')"
        />

        <ul v-else-if="managers.length" class="flex flex-col gap-2">
          <li
            v-for="manager in managers"
            :key="manager.id"
            class="flex flex-col gap-3 rounded-lg border border-border bg-sunken px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
            :data-test="`manager-row-${manager.id}`"
          >
            <div class="flex min-w-0 items-start gap-3">
              <UserAvatar
                :display-name="manager.displayName"
                :username="manager.username"
              />
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="truncate font-medium text-ink">{{ identityName(manager) }}</span>
                  <StatusBadge v-if="manager.disabled" variant="danger">
                    {{ t('admin.account.disabledLabel') }}
                  </StatusBadge>
                </div>
                <p class="truncate font-mono text-xs text-muted">{{ manager.username }}</p>
                <p class="mt-1 text-xs text-muted">
                  {{ t('admin.appManagers.assignedAt') }}
                  <time :datetime="manager.assignedAt">
                    {{ formatDateTime(manager.assignedAt, locale) }}
                  </time>
                </p>
              </div>
            </div>

            <Button
              type="button"
              variant="outline"
              size="sm"
              class="w-full sm:w-auto"
              :disabled="mutationApi.busy.value || managersApi.busy.value"
              :aria-label="t('admin.appManagers.removeAccount', { name: identityName(manager) })"
              :data-test="`manager-remove-${manager.id}`"
              @click="removeManager(manager)"
            >
              {{ t('admin.appManagers.remove') }}
            </Button>
          </li>
        </ul>

        <EmptyState
          v-else-if="!managersApi.error.value"
          :title="t('admin.appManagers.noneAssigned')"
          :description="t('admin.appManagers.noneAssignedDescription')"
        />

        <p
          v-if="managersApi.busy.value && managers.length"
          role="status"
          class="text-sm text-muted"
        >
          {{ t('admin.appManagers.loading') }}
        </p>

        <div v-if="managersApi.error.value" class="flex justify-start">
          <Button
            type="button"
            variant="outline"
            size="sm"
            :disabled="managersApi.busy.value"
            @click="loadManagers"
          >
            {{ t('admin.appManagers.retry') }}
          </Button>
        </div>
      </FormSection>

      <FormSection
        :title="t('admin.appManagers.search')"
        :description="t('admin.appManagers.searchDescription')"
      >
        <ErrorPanel
          :error="accountsApi.error.value"
          :is-admin="true"
          @dismiss="accountsApi.clear"
        />

        <form
          role="search"
          :aria-label="t('admin.appManagers.searchLabel')"
          class="flex flex-col gap-2"
          @submit.prevent="searchAccounts"
        >
          <Label :for="searchInputId">{{ t('admin.appManagers.searchLabel') }}</Label>
          <div class="flex flex-col gap-2 sm:flex-row">
            <Input
              :id="searchInputId"
              v-model="searchQuery"
              type="search"
              class="flex-1"
              data-test="manager-account-search"
              :aria-label="t('admin.appManagers.searchLabel')"
              :placeholder="t('admin.appManagers.searchPlaceholder')"
              @keydown.enter.prevent="searchAccounts"
            />
            <Button
              type="submit"
              class="w-full sm:w-auto"
              :disabled="accountsApi.busy.value"
            >
              {{ t('admin.appManagers.searchAction') }}
            </Button>
          </div>
        </form>

        <p v-if="accountsApi.busy.value" role="status" class="text-sm text-muted">
          {{ t('admin.appManagers.searching') }}
        </p>

        <ul v-else-if="availableAccounts.length" class="flex flex-col gap-2">
          <li
            v-for="account in availableAccounts"
            :key="account.id"
            class="flex flex-col gap-3 rounded-lg border border-border bg-sunken px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
            :data-test="`manager-account-result-${account.id}`"
          >
            <div class="flex min-w-0 items-center gap-3">
              <UserAvatar
                :display-name="account.displayName"
                :username="account.username"
                :src="account.avatarUrl"
              />
              <div class="min-w-0">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="truncate font-medium text-ink">{{ identityName(account) }}</span>
                  <StatusBadge v-if="account.disabled" variant="danger">
                    {{ t('admin.account.disabledLabel') }}
                  </StatusBadge>
                </div>
                <p class="truncate font-mono text-xs text-muted">{{ account.username }}</p>
              </div>
            </div>

            <Button
              type="button"
              size="sm"
              class="w-full sm:w-auto"
              :disabled="account.disabled || mutationApi.busy.value || managersApi.busy.value"
              :aria-label="t('admin.appManagers.assignAccount', { name: identityName(account) })"
              :data-test="`manager-assign-${account.id}`"
              @click="assignManager(account)"
            >
              {{ t('admin.appManagers.assign') }}
            </Button>
          </li>
        </ul>

        <EmptyState
          v-else-if="hasSearched && !accountsApi.error.value"
          :title="t('admin.appManagers.noResults')"
        />

        <p v-else-if="!accountsApi.error.value" class="text-sm text-muted">
          {{ t('admin.appManagers.searchPrompt') }}
        </p>
      </FormSection>
    </CardContent>
  </Card>
</template>
