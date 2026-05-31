<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

const { locale } = useI18n()

onMounted(() => {
  document.documentElement.lang = locale.value
})

function setLocale(next: 'zh' | 'en') {
  locale.value = next
  localStorage.setItem('locale', next)
  document.documentElement.lang = next
}

function toggle() {
  setLocale(locale.value === 'zh' ? 'en' : 'zh')
}
</script>

<template>
  <UButton
    color="neutral"
    variant="ghost"
    size="sm"
    @click="toggle"
  >
    {{ locale === 'zh' ? 'EN' : '中文' }}
  </UButton>
</template>
