<script setup lang="ts">
/**
 * EditProfileDialog — edits the current account avatar and displayName.
 * Avatar: PUT /me/avatar (raw image body) / DELETE /me/avatar.
 * Display name: PUT /me { displayName }.
 * Client validation mirrors the server (1-128, no control chars, NO trim) for
 * the disabled state; the server stays the source of truth and its error
 * surfaces inline. Sudo-free.
 *
 * Two zones, two effect models:
 *   - Avatar zone: changes apply immediately via their own endpoints.
 *   - Display name zone: changes only persist on "Save name".
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
import { Separator } from '@/components/ui/separator'
// Label is intentionally omitted — section headings use plain <p> elements.
import UserAvatar from '@/components/custom/UserAvatar.vue'
import AvatarCropper from '@/components/custom/AvatarCropper.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'

const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean] }>()

const { t } = useI18n()
const auth = useAuthStore()
const { busy, error, run, clear, errorText } = useApi()

const draft = ref('')
const inputRef = ref<{ $el?: HTMLElement }>()
const fileRef = ref<HTMLInputElement>()
const cropSrc = ref<string | null>(null)

// Per-card pending source: the source key currently being switched to, or null.
const pendingSource = ref<string | null>(null)

// Track which zone the last error came from so it surfaces in the right place.
// 'avatar' = upload/remove/selection error; 'name' = PUT /me error.
const errorZone = ref<'avatar' | 'name' | null>(null)

// Reset the draft to the current value each time the dialog opens - no stale
// carry-over - and focus the input (layered on top of reka's focus trap, as
// ConfirmDialog does) so the user can start typing immediately.
watch(() => props.open, (o) => {
  if (o) {
    draft.value = auth.me?.displayName ?? ''
    error.value = null
    errorZone.value = null
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

function onOpenChange(v: boolean): void { emit('update:open', v) }

async function save(): Promise<void> {
  if (!canSave.value) return
  errorZone.value = 'name'
  const result = await run(() =>
    api.put<SessionView>('/api/prohibitorum/me', { displayName: draft.value }),
  )
  if (result) {
    auth.setDisplayName(result.displayName)
    emit('update:open', false)
  }
}

// Resolve an image's natural dimensions, or null if it cannot be loaded.
function loadImageSize(url: string): Promise<{ w: number; h: number } | null> {
  return new Promise((resolve) => {
    const img = new Image()
    img.onload = () => resolve({ w: img.naturalWidth, h: img.naturalHeight })
    img.onerror = () => resolve(null)
    img.src = url
  })
}

async function uploadAvatar(body: Blob): Promise<void> {
  errorZone.value = 'avatar'
  await run(() => api.upload('/api/prohibitorum/me/avatar', body))
  if (!error.value) await auth.reload()
}

async function onFile(e: Event): Promise<void> {
  const f = (e.target as HTMLInputElement).files?.[0]
  if (fileRef.value) fileRef.value.value = ''
  if (!f) return
  if (f.size > 5 * 1024 * 1024) {
    errorZone.value = 'avatar'
    error.value = { code: 'avatar_too_large' }
    return
  }
  error.value = null
  const url = URL.createObjectURL(f)
  const sz = await loadImageSize(url)
  // Already square (1:1): no crop needed - upload as-is (the server crops to a
  // square and resizes to 512). Skip straight to upload.
  if (sz && sz.w > 0 && sz.w === sz.h) {
    URL.revokeObjectURL(url)
    await uploadAvatar(f)
    return
  }
  // Non-square (or dimensions undeterminable): let the user pick the square region.
  cropSrc.value = url
}

function closeCrop(): void {
  if (cropSrc.value) URL.revokeObjectURL(cropSrc.value)
  cropSrc.value = null
}

async function onCropped(blob: Blob): Promise<void> {
  await uploadAvatar(blob)
  closeCrop()
}

async function removeAvatar(): Promise<void> {
  errorZone.value = 'avatar'
  await run(() => api.del('/api/prohibitorum/me/avatar'))
  if (!error.value) await auth.reload()
}

// source is any stored source key: 'user', 'none', or a per-upstream
// 'upstream:<slug>'. The server validates + proves existence.
async function selectSource(source: string): Promise<void> {
  pendingSource.value = source
  errorZone.value = 'avatar'
  await run(() => api.put('/api/prohibitorum/me/avatar/selection', { source }))
  pendingSource.value = null
  if (!error.value) await auth.reload()
}

// The set of sources that have a stored image, derived from avatarSourceUrls.
// Keys present: 'user' and/or per-upstream 'upstream:<slug>'. Always show 'none'.
const sourceEntries = computed(() => {
  const urls = auth.me?.avatarSourceUrls ?? {}
  return Object.entries(urls) as [string, string][]
})

function sourceLabel(key: string): string {
  // Prefer the server-supplied label (the upstream IdP display name); fall back
  // to the generic strings so the picker still reads sensibly without it.
  const serverLabel = auth.me?.avatarSourceLabels?.[key]
  if (serverLabel) return serverLabel
  if (key === 'user') return t('accountMenu.avatarUploaded')
  if (key === 'upstream' || key.startsWith('upstream:')) return t('accountMenu.avatarInherited')
  return key
}

// The active selection; absent (NULL) is treated as 'none'. Single source of
// truth for both the aria-pressed state and the selected-card styling.
const activeSource = computed(() => auth.me?.avatarSource ?? 'none')
</script>

<template>
  <Dialog :open="open" @update:open="onOpenChange">
    <DialogContent class="sm:max-w-md">
      <DialogHeader>
        <DialogTitle>{{ t('accountMenu.editTitle') }}</DialogTitle>
        <DialogDescription>{{ t('accountMenu.editDescription') }}</DialogDescription>
      </DialogHeader>
      <AvatarCropper v-if="cropSrc" :src="cropSrc" @crop="onCropped" @cancel="closeCrop" />
      <form v-else class="flex flex-col gap-4" @submit.prevent="save">

        <!-- ── Avatar zone (changes apply immediately) ─────────────────── -->
        <div class="flex flex-col gap-2">
          <p id="avatar-picker-label" class="text-sm font-medium text-ink">{{ t('accountMenu.avatarLabel') }}</p>

          <!-- Source picker: one card per stored source + always-present None -->
          <div
            role="group"
            aria-labelledby="avatar-picker-label"
            class="flex flex-wrap gap-2"
          >
            <button
              v-for="[key, url] in sourceEntries"
              :key="key"
              type="button"
              :data-test="`avatar-source-${key}`"
              :aria-pressed="activeSource === key"
              :aria-busy="pendingSource === key"
              :disabled="busy"
              :class="[
                'flex flex-col items-center gap-1 rounded-lg border p-2 text-xs transition-colors',
                activeSource === key
                  ? 'border-primary bg-primary/10 font-semibold'
                  : 'border-border hover:bg-accent',
                busy ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
                pendingSource === key ? 'ring-2 ring-primary/40' : '',
              ]"
              @click="selectSource(key)"
            >
              <UserAvatar :display-name="auth.me?.displayName" :username="auth.me?.username" :src="url" class="size-10" />
              <span>{{ sourceLabel(key) }}</span>
            </button>
            <!-- None option — always shown; shows no picture (does not delete the upload) -->
            <button
              type="button"
              data-test="avatar-source-none"
              :aria-pressed="activeSource === 'none'"
              :aria-busy="pendingSource === 'none'"
              :disabled="busy"
              :class="[
                'flex flex-col items-center gap-1 rounded-lg border p-2 text-xs transition-colors',
                activeSource === 'none'
                  ? 'border-primary bg-primary/10 font-semibold'
                  : 'border-border hover:bg-accent',
                busy ? 'opacity-50 cursor-not-allowed' : 'cursor-pointer',
                pendingSource === 'none' ? 'ring-2 ring-primary/40' : '',
              ]"
              @click="selectSource('none')"
            >
              <UserAvatar :display-name="auth.me?.displayName" :username="auth.me?.username" class="size-10" />
              <span>{{ t('accountMenu.avatarNone') }}</span>
            </button>
          </div>
          <span class="text-xs text-muted-foreground">{{ t('accountMenu.avatarSourceHint') }}</span>

          <!-- Upload / Remove row -->
          <div class="flex gap-2 items-start flex-col">
            <div class="flex gap-2">
              <input ref="fileRef" type="file" accept="image/png,image/jpeg,image/webp,image/gif" class="hidden" aria-hidden="true" data-test="avatar-file" @change="onFile" />
              <Button type="button" size="sm" variant="outline" :disabled="busy" data-test="avatar-upload" @click="fileRef?.click()">{{ t('accountMenu.avatarUpload') }}</Button>
              <!-- Remove deletes the stored upload; distinct from "No avatar" which only changes the display selection -->
              <Button
                v-if="auth.me?.avatarSourceUrls?.user"
                type="button"
                size="sm"
                variant="ghost"
                :disabled="busy"
                data-test="avatar-remove"
                :title="t('accountMenu.avatarRemoveHint')"
                @click="removeAvatar"
              >{{ t('accountMenu.avatarRemove') }}</Button>
            </div>
            <!-- Format hint lives directly under the Upload button -->
            <span class="text-xs text-muted-foreground">{{ t('accountMenu.avatarHint') }}</span>
            <!-- Remove disambiguation hint shown when the Remove button is visible -->
            <span v-if="auth.me?.avatarSourceUrls?.user" class="text-xs text-muted-foreground">
              {{ t('accountMenu.avatarRemoveHint') }}
            </span>
          </div>

          <!-- Avatar-zone error (selection/upload/remove errors surface here) -->
          <ErrorPanel v-if="error && errorZone === 'avatar'" :error="error" data-test="avatar-error" @dismiss="clear" />
        </div>

        <Separator />

        <!-- ── Display name zone (persists only on Save name) ──────────── -->
        <div class="flex flex-col gap-1.5">
          <Label for="edit-displayName" class="text-ink">{{ t('accountMenu.displayNameSection') }}</Label>
          <Input
            id="edit-displayName"
            ref="inputRef"
            v-model="draft"
            data-test="edit-displayname-input"
            :maxlength="128"
            :aria-invalid="error && errorZone === 'name' ? true : undefined"
            :aria-describedby="error && errorZone === 'name' ? 'edit-displayName-error' : undefined"
          />
          <!-- Unsaved-name hint when the user has typed but not yet saved -->
          <span v-if="dirty" class="text-xs text-muted-foreground" data-test="unsaved-name-hint">
            {{ t('accountMenu.unsavedName') }}
          </span>
        </div>

        <!-- Name-field error (PUT /me errors surface here) -->
        <ErrorPanel v-if="error && errorZone === 'name'" :error="error" @dismiss="clear" />

        <DialogFooter class="gap-2">
          <!-- "Close" replaces "Cancel": avatar changes are already applied,
               closing only discards the unsaved display name. -->
          <Button type="button" variant="ghost" :disabled="busy" data-test="edit-close" @click="onOpenChange(false)">
            {{ t('common.close') }}
          </Button>
          <!-- Save name: scoped to the display name field only. -->
          <Button type="submit" :disabled="!canSave" data-test="edit-save">
            {{ t('accountMenu.saveName') }}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
</template>
