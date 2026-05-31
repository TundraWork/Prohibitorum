<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()

const code = computed(() => (typeof route.query.code === 'string' ? route.query.code : ''))
const description = computed(() => (typeof route.query.description === 'string' ? route.query.description : ''))
const message = computed(() => {
  if (code.value && te('errors.' + code.value)) return t('errors.' + code.value)
  return description.value || t('error.generic')
})
</script>

<template>
  <UCard class="w-full max-w-md">
    <div class="flex flex-col items-center gap-4 text-center">
      <UIcon name="i-lucide-triangle-alert" class="size-10 text-error" />
      <h1 class="text-xl font-semibold">{{ t('error.title') }}</h1>
      <p role="alert" aria-live="polite" class="text-muted">{{ message }}</p>
      <UButton type="button" @click="() => router.push('/login')">
        {{ t('login.title') }}
      </UButton>
    </div>
  </UCard>
</template>
