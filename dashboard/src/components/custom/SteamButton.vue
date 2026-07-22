<script setup lang="ts">
/** SteamButton — Valve-brand "Sign in through Steam" button: our native Button
 *  shape, black background, white Steam mark + white text. */
import { Button } from '@/components/ui/button'
import SteamLogo from '@/assets/steam-logo.svg'

defineProps<{ label: string }>()
defineEmits<{ (e: 'click'): void }>()
</script>

<template>
  <Button
    type="button"
    data-test="steam-login"
    class="w-full justify-start gap-2 border border-transparent bg-[#232730] text-white hover:bg-[#171a21]"
    @click="$emit('click')"
  >
    <img :src="SteamLogo" alt="" aria-hidden="true" class="size-6" />
    <span>{{ label }}</span>
  </Button>
</template>
