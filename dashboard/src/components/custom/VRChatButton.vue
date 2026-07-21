<script setup lang="ts">
import VRChatLogo from '@/assets/vrchat-logo.svg'
import { Button } from '@/components/ui/button'

defineProps<{ label: string }>()
defineEmits<{ (event: 'click'): void }>()
</script>

<template>
  <Button
    type="button"
    data-test="vrchat-login"
    class="w-full justify-start gap-2 border-0 bg-[#2BAAC1] text-[#0B1A21] hover:bg-[#279AAE] active:bg-[#2592A6] focus-visible:ring-[#2BAAC1]/50"
    @click="$emit('click')"
  >
    <img :src="VRChatLogo" alt="" aria-hidden="true" class="size-5" />
    <span>{{ label }}</span>
  </Button>
</template>
