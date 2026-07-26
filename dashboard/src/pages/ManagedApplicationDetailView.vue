<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import type { AppKind } from '@/lib/appAccess'
import AppPolicyWorkspace from '@/components/custom/AppPolicyWorkspace.vue'
import BackLink from '@/components/custom/BackLink.vue'

const route = useRoute()
const { t } = useI18n()
const kind = computed(() => String(route.params.kind) as AppKind)
const appId = computed(() => String(route.params.id))
</script>

<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <BackLink to="/manage/applications" :label="t('manage.applications.back')" />
    <AppPolicyWorkspace
      :kind="kind"
      :app-id="appId"
      :display-name="appId"
      mode="manager"
    />
  </div>
</template>
