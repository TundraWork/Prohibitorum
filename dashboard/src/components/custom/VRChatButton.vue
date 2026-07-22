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
    class="w-full justify-start gap-2 border border-transparent bg-[#6ae3f9] text-[#0B1A21] hover:bg-[#064b5c] hover:text-white focus-visible:ring-[#6ae3f9]/50"
    @click="$emit('click')"
  >
    <img :src="VRChatLogo" alt="" aria-hidden="true" class="size-6 rounded-md" />
    <span>{{ label }}</span>
  </Button>
</template>
