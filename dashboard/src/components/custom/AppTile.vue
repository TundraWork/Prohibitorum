<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import AppIcon from '@/components/custom/AppIcon.vue'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent, DropdownMenuItem,
} from '@/components/ui/dropdown-menu'
import { MoreVertical, KeyRound } from 'lucide-vue-next'

export interface LaunchpadApp { kind: 'oidc' | 'forward_auth' | 'saml'; id: string; name: string; iconUrl?: string | null; launchUrl: string }
export interface ConsentInfo { scopes: string[] }

const props = defineProps<{ app: LaunchpadApp; consent?: ConsentInfo | null }>()
const emit = defineEmits<{ (e: 'revoke', app: LaunchpadApp): void }>()
const { t } = useI18n()

const typeLabel = computed(() => t(`myApps.type.${props.app.kind}`))
const hasConsent = computed(() => !!props.consent)
</script>

<template>
  <div class="group relative flex flex-col gap-3 rounded-lg border border-line bg-card p-4 transition hover:border-ink/30 hover:shadow-sm">
    <div v-if="hasConsent" class="absolute right-2 top-2 opacity-0 transition group-focus-within:opacity-100 group-hover:opacity-100">
      <DropdownMenu>
        <DropdownMenuTrigger as-child>
          <Button variant="ghost" size="icon" class="size-7" :aria-label="t('myApps.menu')" :data-test="`menu-${app.id}`">
            <MoreVertical class="size-4" />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          <DropdownMenuItem :data-test="`revoke-${app.id}`" @select="emit('revoke', app)">{{ t('myApps.revoke') }}</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </div>

    <a :href="app.launchUrl" target="_blank" rel="noopener" class="flex flex-col items-center gap-3 text-center" :data-test="`launch-${app.id}`">
      <AppIcon :src="app.iconUrl" :name="app.name" size="md" />
      <span class="min-w-0 truncate font-medium text-ink">{{ app.name }}</span>
    </a>

    <div class="flex items-center justify-center gap-2 text-xs text-muted">
      <span class="rounded bg-accent px-1.5 py-0.5">{{ typeLabel }}</span>
      <KeyRound v-if="hasConsent" class="size-3.5" :aria-label="t('myApps.consentGranted')" :data-test="`consent-${app.id}`" />
    </div>
  </div>
</template>
