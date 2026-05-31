<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { api } from '../lib/api'
import { passkeyRegister, type EnrollFields } from '../lib/webauthn'
import LocaleSwitcher from '../components/LocaleSwitcher.vue'

interface EnrollmentPreview {
  intent: string
  target?: { username: string; displayName: string } | null
  expiresAt: string
}

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const token = computed(() => String(route.params.token))

const preview = ref<EnrollmentPreview | null>(null)
const username = ref('')
const displayName = ref('')
const nickname = ref('')
const error = ref('')
const busy = ref(false)

// Bootstrap & invite require the user to choose username + displayName (server
// validates). Reset targets an existing account — no identity inputs.
const needsIdentity = computed(() => preview.value?.intent === 'bootstrap' || preview.value?.intent === 'invite')

const title = computed(() => {
  switch (preview.value?.intent) {
    case 'invite': return t('enroll.titleInvite')
    case 'reset': return t('enroll.titleReset')
    default: return t('enroll.titleBootstrap')
  }
})

function show(e: any) {
  const code = e?.code as string | undefined
  error.value = code && te('errors.' + code) ? t('errors.' + code) : (e?.message ?? t('errors.server_error'))
}

onMounted(async () => {
  try {
    preview.value = await api.get<EnrollmentPreview>(`/api/prohibitorum/enrollments/${encodeURIComponent(token.value)}`)
  } catch (e: any) {
    router.replace({ path: '/error', query: { code: e?.code ?? 'server_error' } })
  }
})

async function register() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const fields: EnrollFields = { nickname: nickname.value || undefined }
    if (needsIdentity.value) {
      fields.username = username.value
      fields.displayName = displayName.value
    }
    await passkeyRegister(token.value, fields)
    // Full navigation so the freshly-set session cookie is in play and the store reloads.
    window.location.assign('/')
  } catch (e) { show(e) } finally { busy.value = false }
}
</script>

<template>
  <div class="min-h-screen flex flex-col items-center justify-center gap-6 p-4 bg-default">
    <header class="w-full max-w-sm flex items-center justify-between">
      <span class="text-lg font-semibold">{{ t('app.name') }}</span>
      <LocaleSwitcher />
    </header>
    <UCard v-if="preview" class="w-full max-w-sm">
      <h1 class="text-xl font-semibold text-center">{{ title }}</h1>
      <p v-if="preview.target" class="text-center text-muted mt-1">
        {{ t('enroll.forTarget', { name: preview.target.displayName }) }}
      </p>

      <div class="mt-6 flex flex-col gap-3">
        <template v-if="needsIdentity">
          <UFormField :label="t('enroll.username')">
            <UInput v-model="username" type="text" autocomplete="username" />
          </UFormField>
          <UFormField :label="t('enroll.displayName')">
            <UInput v-model="displayName" type="text" autocomplete="name" />
          </UFormField>
        </template>
        <UFormField :label="t('enroll.nickname')">
          <UInput v-model="nickname" type="text" />
        </UFormField>

        <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm">{{ error }}</p>

        <UButton data-test="register" type="button" block :loading="busy" :disabled="busy" @click="register">
          {{ busy ? t('enroll.registering') : t('enroll.register') }}
        </UButton>
      </div>
    </UCard>
    <div v-else class="flex justify-center mt-8">
      <UIcon name="i-lucide-loader-2" class="size-8 animate-spin text-muted" />
    </div>
  </div>
</template>
