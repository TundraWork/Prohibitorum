<script setup lang="ts">
/** ProfileView (/) — read-only account profile from the auth store. */
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { Card, CardContent } from '@/components/ui/card'

const { t } = useI18n()
const auth = useAuthStore()
</script>

<template>
  <div class="flex max-w-xl flex-col gap-4">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('profile.title') }}</h1>
    <Card>
      <CardContent class="pt-6">
        <dl v-if="auth.me" class="grid grid-cols-[8rem_1fr] gap-y-3 text-sm">
          <dt class="text-muted">{{ t('profile.username') }}</dt>
          <dd class="font-mono text-ink">{{ auth.me.username }}</dd>
          <dt class="text-muted">{{ t('profile.displayName') }}</dt>
          <dd class="text-ink">{{ auth.me.displayName }}</dd>
          <dt class="text-muted">{{ t('profile.role') }}</dt>
          <dd class="text-ink">{{ auth.me.role }}</dd>
        </dl>
      </CardContent>
    </Card>
  </div>
</template>
