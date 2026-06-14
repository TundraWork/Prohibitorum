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

// Default the stencil to the largest centered square the image allows -
// side = min(width, height) - rather than the library's smaller default.
function defaultSize({ imageSize }: { imageSize: { width: number; height: number } }) {
  const edge = Math.min(imageSize.width, imageSize.height)
  return { width: edge, height: edge }
}
</script>

<template>
  <div class="flex min-w-0 flex-col gap-3">
    <Cropper
      ref="cropperRef"
      :src="src"
      :stencil-props="{ aspectRatio: 1 }"
      :default-size="defaultSize"
      :canvas="{ maxWidth: 1024, maxHeight: 1024 }"
      class="h-64 w-full min-w-0 rounded-md bg-sunken"
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
