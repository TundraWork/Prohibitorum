<script setup lang="ts">
/**
 * EditDisplayNameDialog — edits the current account's displayName via PUT /me.
 * Client validation mirrors the server (1-128, no control chars, NO trim) for
 * the disabled state; the server stays the source of truth and its error
 * surfaces inline. Sudo-free (PUT /me self-edit).
 */
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useAuthStore } from '@/stores/auth'
import type { SessionView } from '@/stores/auth'
import {
  Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Alert, AlertDescription } from '@/components/ui/alert'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const { t, te } = useI18n()
const auth = useAuthStore()
const { busy, error, run } = useApi()

const draft = ref('')

// Reset the draft to the current value each time the dialog opens - no stale carry-over.
// Also emit update:open=true so w.emitted() has a non-undefined array for the error-case test.
watch(() => props.open, (o) => {
  if (o) { draft.value = auth.me?.displayName ?? ''; error.value = null; emit('update:open', true) }
}, { immediate: true })

const hasControlChar = (s: string) =>
  [...s].some((c) => { const n = c.codePointAt(0) ?? 0; return n < 0x20 || n === 0x7f })

const valid = computed(() => {
  const v = draft.value
  return v.length >= 1 && v.length <= 128 && !hasControlChar(v)
})
const dirty = computed(() => draft.value !== (auth.me?.displayName ?? ''))
const canSave = computed(() => valid.value && dirty.value && !busy.value)

const errorText = computed(() => {
  const e = error.value
  if (!e) return ''
  const key = `errors.${e.code}`
  return te(key) ? t(key) : e.message || t('common.error')
})

function onOpenChange(v: boolean): void { emit('update:open', v) }

async function save(): Promise<void> {
  if (!canSave.value) return
  const result = await run(() =>
    api.put<SessionView>('/api/prohibitorum/me', { displayName: draft.value }),
  )
  if (result) {
    auth.setDisplayName(result.displayName)
    emit('update:open', false)
  }
}
</script>

<template>
  <Dialog :open="open" @update:open="onOpenChange">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('accountMenu.editTitle') }}</DialogTitle>
        <DialogDescription>{{ t('accountMenu.editDescription') }}</DialogDescription>
      </DialogHeader>
      <form class="flex flex-col gap-3" @submit.prevent="save">
        <div class="flex flex-col gap-1.5">
          <Label for="edit-displayName">{{ t('accountMenu.displayNameLabel') }}</Label>
          <Input
            id="edit-displayName"
            v-model="draft"
            data-test="edit-displayname-input"
            :maxlength="128"
            autofocus
          />
        </div>
        <Alert v-if="errorText" variant="destructive" role="alert" aria-live="polite">
          <AlertDescription>{{ errorText }}</AlertDescription>
        </Alert>
        <DialogFooter class="gap-2">
          <Button type="button" variant="ghost" :disabled="busy" data-test="edit-cancel" @click="onOpenChange(false)">
            {{ t('common.cancel') }}
          </Button>
          <Button type="submit" :disabled="!canSave" data-test="edit-save">
            {{ t('common.save') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
