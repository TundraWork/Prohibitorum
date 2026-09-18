<script setup lang="ts">
/** TotpQr — renders an otpauth URI as a scannable QR (data: PNG img). */
import { ref, watchEffect } from 'vue'
import QRCode from 'qrcode'

const props = defineProps<{ uri: string; alt: string }>()
const src = ref('')

watchEffect(async () => {
  const uri = props.uri
  if (!uri) { src.value = ''; return }
  try {
    const data = await QRCode.toDataURL(uri, { width: 200, margin: 1 })
    if (props.uri === uri) src.value = data
  } catch {
    if (props.uri === uri) src.value = ''
  }
})
</script>

<template>
  <img v-if="src" :src="src" :alt="alt" width="200" height="200"
       class="rounded-md border border-border bg-bg p-2" />
</template>
