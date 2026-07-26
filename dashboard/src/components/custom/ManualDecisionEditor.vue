<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { AccountSummary, ManualDecision, ManualEffect } from '@/lib/appAccess'

const EFFECTS = ['allow', 'deny'] as const satisfies readonly ManualEffect[]

const props = withDefaults(
  defineProps<{
    decisions: readonly ManualDecision[]
    allDecisions?: readonly ManualDecision[]
    accounts: readonly AccountSummary[]
    busy?: boolean
  }>(),
  { busy: false },
)

const emit = defineEmits<{
  (event: 'set-decision', payload: { accountId: number; effect: ManualEffect }): void
  (event: 'clear-decision', payload: { accountId: number }): void
}>()

const { t } = useI18n()
const activeEffect = ref<ManualEffect>('allow')
const searchQuery = ref('')

const decisionsByEffect = computed<Record<ManualEffect, ManualDecision[]>>(() => ({
  allow: props.decisions.filter((decision) => decision.effect === 'allow'),
  deny: props.decisions.filter((decision) => decision.effect === 'deny'),
}))

const decidedAccountIds = computed(() => {
  const index = props.allDecisions ?? props.decisions
  return new Set(index.map((decision) => decision.account.id))
})

const normalizedSearch = computed(() => searchQuery.value.trim().toLowerCase())

const searchResults = computed(() => {
  const query = normalizedSearch.value
  if (!query) return []

  return props.accounts.filter((account) => {
    if (decidedAccountIds.value.has(account.id)) return false
    return (
      account.displayName.toLowerCase().includes(query) ||
      account.username.toLowerCase().includes(query)
    )
  })
})

function accountName(account: AccountSummary): string {
  return account.displayName.trim() || account.username
}

function effectLabel(effect: ManualEffect): string {
  return t(`manage.policy.manual.${effect}`)
}

function oppositeEffect(effect: ManualEffect): ManualEffect {
  return effect === 'allow' ? 'deny' : 'allow'
}

function setDecisionLabel(account: AccountSummary, effect: ManualEffect): string {
  return t(
    effect === 'allow'
      ? 'manage.policy.manual.setAllowLabel'
      : 'manage.policy.manual.setDenyLabel',
    { name: accountName(account) },
  )
}

function changeDecisionLabel(decision: ManualDecision): string {
  return t(
    decision.effect === 'allow'
      ? 'manage.policy.manual.changeToDenyLabel'
      : 'manage.policy.manual.changeToAllowLabel',
    { name: accountName(decision.account) },
  )
}

function setDecision(accountId: number, effect: ManualEffect): void {
  if (props.busy) return
  emit('set-decision', { accountId, effect })
}

function clearDecision(accountId: number): void {
  if (props.busy) return
  emit('clear-decision', { accountId })
}
</script>

<template>
  <section
    class="min-w-0"
    data-test="manual-decision-editor"
    :aria-busy="props.busy ? 'true' : 'false'"
  >
    <Tabs v-model="activeEffect" class="gap-4">
      <TabsList
        class="w-full border border-border shadow-none sm:w-fit"
        :aria-label="t('manage.policy.manual.tabsLabel')"
      >
        <TabsTrigger
          v-for="effect in EFFECTS"
          :key="effect"
          type="button"
          :value="effect"
          class="shadow-none data-[state=active]:bg-surface data-[state=active]:shadow-none"
          :data-test="`manual-view-${effect}`"
        >
          {{ effectLabel(effect) }}
        </TabsTrigger>
      </TabsList>

      <TabsContent
        v-for="effect in EFFECTS"
        :key="effect"
        :value="effect"
        class="mt-0 flex flex-col gap-3"
      >
        <p
          class="rounded-md border border-border bg-sunken px-4 py-3 text-sm leading-relaxed text-muted"
          :data-test="`manual-${effect}-explanation`"
        >
          {{ t(`manage.policy.manual.${effect}Explanation`) }}
        </p>

        <ul
          v-if="decisionsByEffect[effect].length"
          class="divide-y divide-border overflow-hidden rounded-lg border border-border bg-surface"
          :data-test="`manual-list-${effect}`"
        >
          <li
            v-for="decision in decisionsByEffect[effect]"
            :key="decision.account.id"
            class="flex flex-col gap-3 px-4 py-3 sm:flex-row sm:items-center sm:justify-between"
          >
            <div class="flex min-w-0 flex-col">
              <span class="truncate text-sm font-medium text-ink">
                {{ accountName(decision.account) }}
              </span>
              <span class="truncate font-mono text-xs text-muted">
                {{ decision.account.username }}
              </span>
            </div>

            <div class="grid w-full grid-cols-2 gap-2 sm:flex sm:w-auto sm:shrink-0">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                class="w-full shadow-none sm:w-auto"
                :disabled="props.busy"
                :aria-label="changeDecisionLabel(decision)"
                :data-test="`manual-set-${oppositeEffect(effect)}-${decision.account.id}`"
                @click="setDecision(decision.account.id, oppositeEffect(effect))"
              >
                {{ effectLabel(oppositeEffect(effect)) }}
              </Button>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                class="w-full shadow-none text-muted hover:text-ink sm:w-auto"
                :disabled="props.busy"
                :aria-label="t('manage.policy.manual.clearLabel', { name: accountName(decision.account) })"
                :data-test="`manual-clear-${decision.account.id}`"
                @click="clearDecision(decision.account.id)"
              >
                {{ t('manage.policy.manual.clear') }}
              </Button>
            </div>
          </li>
        </ul>

        <div
          v-else
          role="status"
          class="rounded-lg border border-border bg-sunken px-4 py-5"
          :data-test="`manual-list-${effect}`"
        >
          <p class="text-sm text-muted" data-test="manual-neutral-copy">
            {{ t('manage.policy.manual.neutral') }}
          </p>
        </div>
      </TabsContent>

      <div class="flex flex-col gap-3 rounded-lg border border-border bg-sunken p-4">
        <label for="manual-account-search" class="text-sm font-medium text-ink">
          {{ t('manage.policy.manual.searchLabel') }}
        </label>
        <Input
          id="manual-account-search"
          v-model="searchQuery"
          type="search"
          autocomplete="off"
          :spellcheck="false"
          class="bg-surface shadow-none"
          :placeholder="t('manage.policy.manual.searchPlaceholder')"
          :aria-label="t('manage.policy.manual.searchLabel')"
          :disabled="props.busy"
          data-test="manual-account-search"
        />

        <ul
          v-if="searchResults.length"
          class="divide-y divide-border border-t border-border"
        >
          <li
            v-for="account in searchResults"
            :key="account.id"
            class="flex flex-col gap-3 py-3 last:pb-0 sm:flex-row sm:items-center sm:justify-between"
            :data-test="`manual-account-result-${account.id}`"
          >
            <div class="flex min-w-0 flex-col">
              <span class="truncate text-sm font-medium text-ink">
                {{ accountName(account) }}
              </span>
              <span class="truncate font-mono text-xs text-muted">
                {{ account.username }}
              </span>
            </div>
            <Button
              type="button"
              variant="secondary"
              size="sm"
              class="w-full shadow-none sm:w-auto sm:shrink-0"
              :disabled="props.busy"
              :aria-label="setDecisionLabel(account, activeEffect)"
              :data-test="`manual-set-${activeEffect}-${account.id}`"
              @click="setDecision(account.id, activeEffect)"
            >
              {{ effectLabel(activeEffect) }}
            </Button>
          </li>
        </ul>

        <p v-else-if="normalizedSearch" role="status" class="text-sm text-muted">
          {{ t('manage.policy.manual.noMatches') }}
        </p>
      </div>
    </Tabs>
  </section>
</template>
