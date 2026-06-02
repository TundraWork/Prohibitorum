<script setup lang="ts">
// TODO(i18n): English-literal scaffold copy; key + translate after the design-system pass.
import PasskeysCard from './security/PasskeysCard.vue'
import PasswordCard from './security/PasswordCard.vue'
import TotpCard from './security/TotpCard.vue'
</script>

<template>
  <div class="space-y-6 max-w-3xl">
    <h1 class="text-lg font-semibold">Security</h1>
    <PasskeysCard />
    <PasswordCard />
    <TotpCard />
  </div>
</template>
