<script setup lang="ts">
/** AvatarCropper — square (1:1) crop of a chosen image; emits a JPEG Blob. */
import { ref } from 'vue'
import { Cropper } from 'vue-advanced-cropper'
import 'vue-advanced-cropper/dist/style.css'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'

defineProps<{ src: string }>()
const emit = defineEmits<{ crop: [Blob]; cancel: [] }>()
const { t } = useI18n()

// Template ref to the cropper instance (getResult() returns { canvas }).
const cropperRef = ref<{ getResult: () => { canvas: HTMLCanvasElement | null } }>()

function useCrop(): void {
  const canvas = cropperRef.value?.getResult().canvas
  if (!canvas) return
  canvas.toBlob((blob) => { if (blob) emit('crop', blob) }, 'image/jpeg', 0.92)
}
</script>

<template>
  <div class="flex flex-col gap-3">
    <Cropper
      ref="cropperRef"
      :src="src"
      :stencil-props="{ aspectRatio: 1 }"
      :canvas="{ maxWidth: 1024, maxHeight: 1024 }"
      class="h-64 w-full rounded-md bg-sunken"
      data-test="avatar-cropper"
    />
    <div class="flex justify-end gap-2">
      <Button type="button" variant="ghost" size="sm" data-test="crop-cancel" @click="emit('cancel')">
        {{ t('common.cancel') }}
      </Button>
      <Button type="button" size="sm" data-test="crop-use" @click="useCrop">
        {{ t('accountMenu.avatarUse') }}
      </Button>
    </div>
  </div>
</template>
