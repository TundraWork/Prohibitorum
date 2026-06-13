<script setup lang="ts">
/**
 * EditProfileDialog — edits the current account avatar and displayName.
 * Avatar: PUT /me/avatar (raw image body) / DELETE /me/avatar.
 * Display name: PUT /me { displayName }.
 * Client validation mirrors the server (1-128, no control chars, NO trim) for
 * the disabled state; the server stays the source of truth and its error
 * surfaces inline. Sudo-free.
 */
import { computed, nextTick, ref, watch } from 'vue'
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
import UserAvatar from '@/components/custom/UserAvatar.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const { t, te } = useI18n()
const auth = useAuthStore()
const { busy, error, run } = useApi()

const draft = ref('')
const inputRef = ref<{ $el?: HTMLElement }>()
const fileRef = ref<HTMLInputElement>()

// Reset the draft to the current value each time the dialog opens - no stale
// carry-over - and focus the input (layered on top of reka's focus trap, as
// ConfirmDialog does) so the user can start typing immediately.
watch(() => props.open, (o) => {
  if (o) {
    draft.value = auth.me?.displayName ?? ''
    error.value = null
    void nextTick(() => inputRef.value?.$el?.focus())
  }
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

async function onFile(e: Event): Promise<void> {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (fileRef.value) fileRef.value.value = ''
  if (!f) return
  if (f.size > 5 * 1024 * 1024) {
    error.value = { code: 'avatar_too_large_client', message: t('accountMenu.avatarTooLargeClient') }
    return
  }
  await run(() => api.upload('/api/prohibitorum/me/avatar', f))
  if (!error.value) await auth.reload()
}

async function removeAvatar(): Promise<void> {
  await run(() => api.del('/api/prohibitorum/me/avatar'))
  if (!error.value) await auth.reload()
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
        <div class="flex items-center gap-3">
          <UserAvatar :display-name="auth.me?.displayName" :username="auth.me?.username" :src="auth.me?.avatarUrl" class="size-16 text-xl" />
          <div class="flex flex-col gap-1.5">
            <span class="text-sm text-muted">{{ t('accountMenu.avatarLabel') }}</span>
            <div class="flex gap-2">
              <input ref="fileRef" type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="hidden" data-test="avatar-file" @change="onFile" />
              <Button type="button" size="sm" variant="outline" :disabled="busy" data-test="avatar-upload" @click="fileRef?.click()">{{ t('accountMenu.avatarUpload') }}</Button>
              <Button v-if="auth.me?.avatarUrl" type="button" size="sm" variant="ghost" :disabled="busy" data-test="avatar-remove" @click="removeAvatar">{{ t('accountMenu.avatarRemove') }}</Button>
            </div>
            <span class="text-xs text-muted">{{ t('accountMenu.avatarHint') }}</span>
          </div>
        </div>
        <div class="flex flex-col gap-1.5">
          <Label for="edit-displayName">{{ t('accountMenu.displayNameLabel') }}</Label>
          <Input
            id="edit-displayName"
            ref="inputRef"
            v-model="draft"
            data-test="edit-displayname-input"
            :maxlength="128"
            :aria-invalid="errorText ? true : undefined"
            :aria-describedby="errorText ? 'edit-displayName-error' : undefined"
          />
        </div>
        <Alert v-if="errorText" id="edit-displayName-error" variant="destructive" aria-live="polite">
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
