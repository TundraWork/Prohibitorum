<script setup lang="ts">
import { computed, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useResource } from '@/composables/useResource'
import { managerQuery } from '@/queries/access'
import { invalidateResource } from '@/queries/invalidation'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useQueryClient } from '@tanstack/vue-query'
import { withSudo } from '@/lib/sudo'
import { formatDateTime } from '@/lib/time'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import FormSection from '@/components/custom/FormSection.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import AccountDirectoryPicker, { type AccountDirectoryEntry } from '@/components/custom/AccountDirectoryPicker.vue'

type AppKind = 'oidc' | 'forward_auth' | 'saml'

interface AppManagerView {
  id: number
  username: string
  displayName: string
  disabled: boolean
  assignedAt: string
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
const queryClient = useQueryClient()
const managersApi = useResource(computed(() => managerQuery<AppManagerView[]>(props.kind, props.appId)))
const mutationApi = useApi()

const managers = computed(() => managersApi.data.value ?? [])

const managerEndpoint = computed(() =>
  `/api/prohibitorum/${MANAGER_COLLECTIONS[props.kind]}/${encodeURIComponent(props.appId)}/managers`,
)

const assignedManagerIds = computed(() => new Set(managers.value.map((manager) => manager.id)))
const displayedManagerError = computed(() => mutationApi.error.value ?? managersApi.error.value)
const initialManagersLoading = computed(() => managersApi.busy.value && managers.value.length === 0)

let active = true
let identityVersion = 0
function identityName(account: Pick<AccountDirectoryEntry, 'displayName' | 'username'>): string {
  return account.displayName || account.username
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

  await invalidateResource(queryClient, 'access')
}

function assignManager(account: AccountDirectoryEntry): Promise<void> | undefined {
  if (assignedManagerIds.value.has(account.id)) return
  return mutateManager(managerEndpoint.value, account.id)
}

function removeManager(manager: AppManagerView): Promise<void> {
  return mutateManager(`${managerEndpoint.value}/remove`, manager.id)
}

function clearManagerError(): void {
  mutationApi.clear()
  managersApi.clear()
}

watch([() => props.kind, () => props.appId], () => {
  identityVersion++
  managersApi.clear(); mutationApi.clear()
})
onBeforeUnmount(() => { active = false; identityVersion++ })
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
            @click="managersApi.refetch()"
          >
            {{ t('admin.appManagers.retry') }}
          </Button>
        </div>
      </FormSection>

      <AccountDirectoryPicker
        :key="`${kind}:${appId}`"
        :excluded-account-ids="[...assignedManagerIds]"
        :busy="mutationApi.busy.value || managersApi.busy.value"
        :title="t('admin.appManagers.search')"
        :description="t('admin.appManagers.searchDescription')"
        :search-label="t('admin.appManagers.searchLabel')"
        :search-placeholder="t('admin.appManagers.searchPlaceholder')"
        :search-action="t('admin.appManagers.searchAction')"
        :searching-label="t('admin.appManagers.searching')"
        :no-results-label="t('admin.appManagers.noResults')"
        :search-prompt="t('admin.appManagers.searchPrompt')"
        :select-label="t('admin.appManagers.assign')"
        :select-aria-label="account => t('admin.appManagers.assignAccount', { name: identityName(account) })"
        test-prefix="manager-account"
        @select="assignManager"
      />
    </CardContent>
  </Card>
</template>
