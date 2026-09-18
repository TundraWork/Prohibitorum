<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Trash2 } from 'lucide-vue-next'
import AccountDirectoryPicker, { type AccountDirectoryEntry } from '@/components/custom/AccountDirectoryPicker.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { ManualDecision, ManualEffect } from '@/lib/appAccess'
import { formatDateTime } from '@/lib/time'

const EFFECTS = ['allow', 'deny'] as const satisfies readonly ManualEffect[]

const props = withDefaults(
  defineProps<{
    decisions: readonly ManualDecision[]
    busy?: boolean
  }>(),
  { busy: false },
)

const emit = defineEmits<{
  (event: 'set-decision', payload: { accountId: number; effect: ManualEffect }): void
  (event: 'clear-decision', payload: { accountId: number }): void
}>()

const { locale, t } = useI18n()
const activeEffect = ref<ManualEffect>('allow')

const decisionsByEffect = computed<Record<ManualEffect, ManualDecision[]>>(() => ({
  allow: props.decisions.filter((decision) => decision.effect === 'allow'),
  deny: props.decisions.filter((decision) => decision.effect === 'deny'),
}))

const decidedAccountIds = computed(() => {
  return new Set(props.decisions.map((decision) => decision.account.id))
})

function accountName(account: Pick<AccountDirectoryEntry, 'displayName' | 'username'>): string {
  return account.displayName.trim() || account.username
}

function effectLabel(effect: ManualEffect): string {
  return t(`manage.policy.manual.${effect}`)
}

function setDecisionLabel(account: AccountDirectoryEntry, effect: ManualEffect): string {
  return t(
    effect === 'allow'
      ? 'manage.policy.manual.setAllowLabel'
      : 'manage.policy.manual.setDenyLabel',
    { name: accountName(account) },
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

function selectAccount(account: AccountDirectoryEntry): void {
  setDecision(account.id, activeEffect.value)
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

        <Table
          v-if="decisionsByEffect[effect].length"
          class="min-w-[40rem]"
          :data-test="`manual-list-${effect}`"
        >
          <TableHeader>
            <TableRow>
              <TableHead>{{ t('manage.policy.manual.user') }}</TableHead>
              <TableHead>{{ t('manage.policy.manual.username') }}</TableHead>
              <TableHead>{{ t('manage.policy.manual.addedAt') }}</TableHead>
              <TableHead><span class="sr-only">{{ t('manage.policy.manual.clear') }}</span></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            <TableRow
              v-for="decision in decisionsByEffect[effect]"
              :key="decision.account.id"
              :data-test="`manual-row-${decision.account.id}`"
            >
              <TableCell>
                <div class="flex min-w-0 items-center gap-3">
                  <UserAvatar :display-name="decision.account.displayName" :username="decision.account.username" />
                  <span class="max-w-56 truncate font-medium text-ink">{{ accountName(decision.account) }}</span>
                </div>
              </TableCell>
              <TableCell class="font-mono text-xs text-muted">{{ decision.account.username }}</TableCell>
              <TableCell class="text-sm text-muted">
                <time :datetime="decision.updatedAt">{{ formatDateTime(decision.updatedAt, locale) }}</time>
              </TableCell>
              <TableCell class="text-right">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  class="text-muted hover:text-destructive"
                  :disabled="props.busy"
                  :title="t('manage.policy.manual.clearLabel', { name: accountName(decision.account) })"
                  :aria-label="t('manage.policy.manual.clearLabel', { name: accountName(decision.account) })"
                  :data-test="`manual-clear-${decision.account.id}`"
                  @click="clearDecision(decision.account.id)"
                >
                  <Trash2 />
                </Button>
              </TableCell>
            </TableRow>
          </TableBody>
        </Table>

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

    </Tabs>

    <AccountDirectoryPicker
      :key="activeEffect"
      :excluded-account-ids="[...decidedAccountIds]"
      :busy="props.busy"
      :title="t('manage.policy.manual.addTitle')"
      :description="t('manage.policy.manual.addDescription')"
      :search-label="t('manage.policy.manual.searchLabel')"
      :search-placeholder="t('manage.policy.manual.searchPlaceholder')"
      :search-action="t('manage.policy.manual.searchAction')"
      :searching-label="t('manage.policy.manual.searching')"
      :no-results-label="t('manage.policy.manual.noMatches')"
      :search-prompt="t('manage.policy.manual.searchPrompt')"
      :select-label="t(activeEffect === 'allow' ? 'manage.policy.manual.addAllow' : 'manage.policy.manual.addDeny')"
      :select-aria-label="account => setDecisionLabel(account, activeEffect)"
      primary-select
      test-prefix="manual-account"
      @select="selectAccount"
    />
  </section>
</template>
