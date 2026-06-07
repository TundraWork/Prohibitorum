<script setup lang="ts">
/**
 * ConfirmDialog — reusable destructive confirmation over the vendored Dialog
 * primitive. Restates the action (title) + itemized consequences (default
 * slot), a red descriptive confirm button, and a Cancel that gets initial
 * focus and is spatially separated. Closing the dialog = cancel.
 */
import { nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle, DialogFooter,
} from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'

const props = defineProps<{ open: boolean; title: string; confirmLabel: string; busy?: boolean }>()
const emit = defineEmits<{ 'update:open': [boolean]; confirm: []; cancel: [] }>()

const { t } = useI18n()
const cancelRef = ref<{ $el?: HTMLElement }>()

function onCancel(): void {
  emit('update:open', false)
  emit('cancel')
}

// Closing via the Dialog (X / Esc / overlay) routes through here too.
function onOpenChange(v: boolean): void {
  emit('update:open', v)
  if (!v) emit('cancel')
}

// Best-effort: nudge focus to Cancel (the safe option) when the dialog opens.
// reka-ui's Dialog runs its own focus-trap on open (focus enters the dialog),
// which is the authoritative a11y behavior; this is a hint layered on top.
watch(() => props.open, async (o) => {
  if (o) { await nextTick(); cancelRef.value?.$el?.focus() }
})
</script>

<template>
  <Dialog :open="open" @update:open="onOpenChange">
    <DialogContent>
      <DialogHeader>
        <DialogTitle>{{ title }}</DialogTitle>
      </DialogHeader>
      <DialogDescription class="text-sm text-ink"><slot /></DialogDescription>
      <DialogFooter class="gap-2">
        <Button ref="cancelRef" type="button" variant="ghost" :disabled="busy" @click="onCancel">
          {{ t('confirm.cancel') }}
        </Button>
        <Button type="button" variant="destructive" :disabled="busy" @click="emit('confirm')">
          {{ confirmLabel }}
        </Button>
      </DialogFooter>
    </DialogContent>
  </Dialog>
</template>
