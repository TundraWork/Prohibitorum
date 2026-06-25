<script setup lang="ts">
/**
 * ConsentScopeList — the permissions an OIDC client is requesting, one row each:
 * a per-scope icon chip, the human label, and a one-line description of what the
 * grant means. Known scopes are described via consent.scopes.<scope> +
 * consent.scopeDesc.<scope>; an unknown / technical scope falls back to its raw
 * value in mono (Code-Gets-Mono rule) with a generic description, so a relying
 * party requesting a custom scope still shows something honest.
 *
 * newScopes — optional list of scopes that are newly requested (incremental
 * consent). Those scopes receive a "New" badge next to their label.
 */
import type { Component } from 'vue'
import { useI18n } from 'vue-i18n'
import { BadgeCheck, User, Mail, Users, Clock, KeyRound } from 'lucide-vue-next'

defineProps<{ scopes: string[]; newScopes?: string[] }>()

const { t, te } = useI18n()
const isKnown = (scope: string) => te(`consent.scopes.${scope}`)

// Per-scope glyph so each permission reads as its own thing, not a uniform list.
const icons: Record<string, Component> = {
  openid: BadgeCheck,
  profile: User,
  email: Mail,
  groups: Users,
  offline_access: Clock,
}
const iconFor = (scope: string): Component => icons[scope] ?? KeyRound
</script>

<template>
  <ul class="flex flex-col gap-1">
    <li v-for="scope in scopes" :key="scope" class="flex items-start gap-3 rounded-lg px-1 py-1.5">
      <span class="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-tide">
        <component :is="iconFor(scope)" class="size-[1.125rem]" aria-hidden="true" />
      </span>
      <div class="min-w-0 pt-0.5">
        <p class="flex items-center gap-1.5 text-sm font-medium text-ink">
          <span v-if="isKnown(scope)">{{ t(`consent.scopes.${scope}`) }}</span>
          <code v-else class="font-mono text-ink">{{ scope }}</code>
          <span
            v-if="newScopes?.includes(scope)"
            class="rounded-full bg-tide-50 px-1.5 py-0.5 text-[0.625rem] font-medium text-tide-700"
          >{{ t('consent.newBadge') }}</span>
        </p>
        <p class="mt-0.5 text-xs text-muted">
          {{ isKnown(scope) ? t(`consent.scopeDesc.${scope}`) : t('consent.customScope') }}
        </p>
      </div>
    </li>
  </ul>
</template>
