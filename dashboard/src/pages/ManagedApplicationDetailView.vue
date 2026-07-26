<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import { api } from '@/lib/api'
import type { AppAccessWorkspace } from '@/lib/appAccess'
import { useApi } from '@/composables/useApi'
import BackLink from '@/components/custom/BackLink.vue'
import CardSkeleton from '@/components/custom/CardSkeleton.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import ProtocolBadge from '@/components/custom/ProtocolBadge.vue'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

const route = useRoute()
const { t } = useI18n()
const { busy, error, run, clear } = useApi()
const workspace = ref<AppAccessWorkspace | null>(null)
const notFound = ref(false)


onMounted(async () => {
  const kind = String(route.params.kind)
  const appId = String(route.params.id)
  const loaded = await run(() => api.get<AppAccessWorkspace>(
    `/api/prohibitorum/managed-applications/${encodeURIComponent(kind)}/${encodeURIComponent(appId)}/access`,
  ))
  if (loaded) {
    workspace.value = loaded
    return
  }
  if (error.value?.code === 'client_not_found') {
    notFound.value = true
    clear()
  }
})
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-6">
    <BackLink to="/manage/applications" :label="t('manage.applications.back')" />
    <ErrorPanel v-if="error && !notFound" :error="error" @dismiss="clear" />
    <p v-if="notFound" class="text-sm text-muted" role="status">{{ t('manage.applications.notFound') }}</p>
    <CardSkeleton v-else-if="busy && !workspace" />

    <template v-else-if="workspace">
      <div class="flex min-w-0 items-start justify-between gap-4" data-test="managed-application-detail">
        <div class="min-w-0">
          <h1 class="truncate text-2xl font-semibold tracking-tight text-ink">{{ workspace.app.displayName }}</h1>
          <p class="mt-1 text-sm text-muted">{{ t('manage.applications.detailSubtitle') }}</p>
        </div>
        <ProtocolBadge :kind="workspace.app.kind" class="mt-1 rounded-md bg-sunken p-2" />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{{ t('manage.applications.accessTitle') }}</CardTitle>
        </CardHeader>
        <CardContent class="flex flex-col gap-5">
          <div class="flex flex-col items-start gap-2">
            <StatusBadge :variant="workspace.accessRestricted ? 'caution' : 'success'">
              {{ workspace.accessRestricted ? t('manage.applications.restricted') : t('manage.applications.open') }}
            </StatusBadge>
            <p class="text-sm text-muted">
              {{ workspace.accessRestricted ? t('manage.applications.restrictedDescription') : t('manage.applications.openDescription') }}
            </p>
          </div>

          <dl class="grid gap-4 border-t border-border pt-5 sm:grid-cols-2">
            <div class="flex flex-col gap-1">
              <dt class="text-xs font-medium uppercase tracking-wide text-muted">{{ t('manage.applications.manualGroup') }}</dt>
              <dd class="text-sm font-medium text-ink">
                {{ workspace.manualGroup ? t('manage.applications.manualGroupReady', { name: workspace.manualGroup.displayName }) : t('manage.applications.noManualGroup') }}
              </dd>
            </div>
            <div class="flex flex-col gap-1">
              <dt class="text-xs font-medium uppercase tracking-wide text-muted">{{ t('admin.nav.groups') }}</dt>
              <dd class="text-sm font-medium text-ink">{{ t('manage.applications.ruleGroups', workspace.ruleGroups.length) }}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>
    </template>
  </div>
</template>
