<script setup lang="ts">
import { Input } from '@/components/ui/input'

const model = defineModel<string>({ default: '' })
</script>

<template>
  <!-- Service credentials are not login passwords. Password managers may opt
       out of honoring these hints according to their own user settings. -->
  <Input
    v-model="model"
    type="password"
    autocomplete="off"
    autocapitalize="none"
    :spellcheck="false"
    data-1p-ignore
    data-lpignore="true"
    data-bwignore
    data-form-type="other"
  />
</template>
