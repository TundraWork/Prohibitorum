<script lang="ts">
export interface AccountIdentity {
  id: number
  providerSlug: string
  providerDisplayName: string
  protocol: string
  subject: string
  email?: string
  data: Record<string, string>
  linkedAt: string
}
</script>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

interface MetadataRow {
  key: string
  label: string
  value: string
  exact: boolean
}

const props = defineProps<{ identity: AccountIdentity }>()
const { t } = useI18n()

const protocolLabel = computed(() => {
  switch (props.identity.protocol) {
    case 'oidc': return t('identity.protocolOidc')
    case 'steam': return t('identity.protocolSteam')
    case 'vrchat': return t('identity.protocolVrchat')
    default: return props.identity.protocol
  }
})

const rows = computed<MetadataRow[]>(() => {
  const identity = props.identity
  const metadata: MetadataRow[] = [
    { key: 'protocol', label: t('identity.protocol'), value: protocolLabel.value, exact: false },
  ]

  switch (identity.protocol) {
    case 'steam':
      metadata.push({ key: 'steamId', label: t('identity.steamId'), value: identity.subject, exact: true })
      if (identity.data.personaName) metadata.push({ key: 'personaName', label: t('identity.personaName'), value: identity.data.personaName, exact: false })
      if (identity.data.profileUrl) metadata.push({ key: 'profileUrl', label: t('identity.profileUrl'), value: identity.data.profileUrl, exact: true })
      break
    case 'vrchat':
      metadata.push({ key: 'userId', label: t('identity.vrchatUserId'), value: identity.subject, exact: true })
      if (identity.data.displayName) metadata.push({ key: 'displayName', label: t('identity.displayName'), value: identity.data.displayName, exact: false })
      if (identity.data.profileUrl) metadata.push({ key: 'profileUrl', label: t('identity.profileUrl'), value: identity.data.profileUrl, exact: true })
      break
    default:
      metadata.push({ key: 'subject', label: t('identity.subject'), value: identity.subject, exact: true })
      if (identity.email) metadata.push({ key: 'email', label: t('identity.email'), value: identity.email, exact: false })
  }

  return metadata
})
</script>

<template>
  <dl class="flex min-w-0 flex-col gap-1 text-sm">
    <div v-for="row in rows" :key="row.key" class="flex min-w-0 items-baseline gap-3">
      <dt class="shrink-0 text-xs text-muted">{{ row.label }}</dt>
      <dd
        :data-test="`identity-${row.key}`"
        class="min-w-0 truncate text-ink"
        :class="row.exact ? 'font-mono text-xs' : ''"
        :title="row.value"
      >{{ row.value }}</dd>
    </div>
  </dl>
</template>
