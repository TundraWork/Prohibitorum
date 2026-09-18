<script setup lang="ts">
/**
 * ProtocolBadge — how an app authenticates, shown as a single corner glyph whose
 * plain-language description appears on hover/focus (tooltip) and is always
 * available to screen readers (sr-only). The protocol is distinguished by ICON +
 * tooltip, never by colour: Sage/Amber/Rose are reserved for credential STATE
 * (the State-Has-a-Colour rule), so a protocol kind must not borrow a state hue.
 *
 * Visual chrome (the chip background, size, position) is supplied by the caller
 * via `class`, so the same glyph can sit over a coloured cover or a plain row.
 */
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { KeyRound, Network, Fingerprint } from 'lucide-vue-next'
import { cn } from '@/lib/utils'
import {
  Tooltip, TooltipTrigger, TooltipContent, TooltipProvider,
} from '@/components/ui/tooltip'

const props = defineProps<{ kind: 'oidc' | 'forward_auth' | 'saml'; class?: string }>()
const { t } = useI18n()

const ICONS = { oidc: KeyRound, forward_auth: Network, saml: Fingerprint } as const
const icon = computed(() => ICONS[props.kind])
const label = computed(() => t(`myApps.type.${props.kind}`))
const hint = computed(() => t(`myApps.typeHint.${props.kind}`))
</script>

<template>
  <TooltipProvider :delay-duration="150">
    <Tooltip>
      <TooltipTrigger
        type="button"
        :data-test="`protocol-${kind}`"
        :aria-label="hint"
        :class="cn('inline-flex cursor-help items-center justify-center outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring', props.class)"
        @click.prevent
      >
        <component :is="icon" class="size-4" aria-hidden="true" />
        <span class="sr-only">{{ label }} — {{ hint }}</span>
      </TooltipTrigger>
      <TooltipContent>{{ hint }}</TooltipContent>
    </Tooltip>
  </TooltipProvider>
</template>
