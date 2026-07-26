<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import type { ManagedApplication } from '@/lib/appAccess'
import { useApi } from '@/composables/useApi'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ProtocolBadge from '@/components/custom/ProtocolBadge.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

const { t } = useI18n()
const { busy, error, run, clear } = useApi()
const applications = ref<ManagedApplication[]>([])

const sortedApplications = computed(() => [...applications.value].sort((a, b) => a.displayName.localeCompare(b.displayName)))

function applicationPath(app: ManagedApplication): string {
  return `/manage/applications/${encodeURIComponent(app.kind)}/${encodeURIComponent(app.appId)}`
}

onMounted(() => {
  void run(async () => {
    applications.value = await api.get<ManagedApplication[]>('/api/prohibitorum/managed-applications')
  })
})
</script>

<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex flex-col gap-1">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('manage.applications.title') }}</h1>
      <p class="text-sm text-muted">{{ t('manage.applications.subtitle') }}</p>
    </div>

    <ErrorPanel :error="error" @dismiss="clear" />
    <TableSkeleton v-if="busy && applications.length === 0" :rows="3" :cols="1" />
    <EmptyState
      v-else-if="!error && applications.length === 0"
      :title="t('manage.applications.emptyTitle')"
      :description="t('manage.applications.emptyDescription')"
    />
    <div v-else-if="applications.length > 0" class="grid gap-3 sm:grid-cols-2" data-test="managed-application-list">
      <Card v-for="app in sortedApplications" :key="`${app.kind}:${app.appId}`" class="h-full">
        <CardContent class="flex h-full flex-col gap-4 pt-6">
          <div class="flex min-w-0 items-start justify-between gap-3">
            <div class="min-w-0">
              <h2 class="truncate font-semibold text-ink">{{ app.displayName }}</h2>
              <p class="truncate text-xs text-muted">{{ app.appId }}</p>
            </div>
            <ProtocolBadge :kind="app.kind" />
          </div>
          <div class="flex flex-wrap gap-2">
            <StatusBadge :variant="app.accessRestricted ? 'caution' : 'success'">
              {{ app.accessRestricted ? t('manage.applications.restricted') : t('manage.applications.open') }}
            </StatusBadge>
          </div>
          <Button as-child variant="outline" class="mt-auto w-full">
            <RouterLink :to="applicationPath(app)">{{ t('manage.applications.view') }}</RouterLink>
          </Button>
        </CardContent>
      </Card>
    </div>
  </div>
</template>
