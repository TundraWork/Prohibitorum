<script setup lang="ts">
/** ProfileView (/) — account profile; displayName is editable via PUT /me. */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import type { SessionView } from '@/stores/auth'
import { Card, CardContent } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import StatusBadge from '@/components/custom/StatusBadge.vue'

const { t, te } = useI18n()
const auth = useAuthStore()
const { busy, error, run } = useApi()

const editing = ref(false)
const draft = ref('')

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function startEdit(): void {
  draft.value = auth.me?.displayName ?? ''
  editing.value = true
}

function cancelEdit(): void {
  editing.value = false
  draft.value = ''
}

async function save(): Promise<void> {
  const result = await run(() =>
    api.put<SessionView>('/api/prohibitorum/me', { displayName: draft.value }),
  )
  if (result) {
    auth.setDisplayName(result.displayName)
    editing.value = false
  }
}
</script>

<template>
  <div class="flex max-w-xl flex-col gap-4">
    <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('profile.title') }}</h1>
    <Card>
      <CardContent class="pt-6">
        <dl v-if="auth.me" class="grid grid-cols-[8rem_1fr] items-center gap-y-4 text-sm">
          <dt class="text-muted">{{ t('profile.username') }}</dt>
          <dd class="truncate font-mono text-ink">{{ auth.me.username }}</dd>

          <dt class="text-muted">{{ t('profile.displayName') }}</dt>
          <dd class="min-w-0">
            <template v-if="editing">
              <div class="flex items-center gap-2">
                <Input
                  v-model="draft"
                  data-test="profile-displayName-input"
                  class="h-7 py-0 text-sm"
                  @keydown.enter.prevent="save"
                  @keydown.escape.prevent="cancelEdit"
                />
                <Button
                  type="button"
                  size="sm"
                  :disabled="busy"
                  data-test="profile-save"
                  @click="save"
                >{{ t('profile.save') }}</Button>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  :disabled="busy"
                  data-test="profile-cancel"
                  @click="cancelEdit"
                >{{ t('profile.cancel') }}</Button>
              </div>
              <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite" class="mt-2">
                <AlertDescription>{{ errorText }}</AlertDescription>
              </Alert>
            </template>
            <template v-else>
              <div class="flex items-center gap-2">
                <span class="truncate text-ink">{{ auth.me.displayName }}</span>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  data-test="profile-edit"
                  :aria-label="t('profile.edit') + ' ' + t('profile.displayName')"
                  @click="startEdit"
                >{{ t('profile.edit') }}</Button>
              </div>
            </template>
          </dd>

          <dt class="text-muted">{{ t('profile.role') }}</dt>
          <dd>
            <StatusBadge :variant="auth.me.role === 'admin' ? 'caution' : 'neutral'" class="capitalize">
              {{ auth.me.role }}
            </StatusBadge>
          </dd>
        </dl>
      </CardContent>
    </Card>
  </div>
</template>
