<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { RotateCcw } from 'lucide-vue-next'
import type { ProviderDescriptor, Rule, RulePreviewPage } from '@/lib/appAccess'
import { cloneRule, validateRule } from '@/lib/ruleDraft'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { Button } from '@/components/ui/button'
import PaginationControls from './PaginationControls.vue'
import StatusBadge from './StatusBadge.vue'

const props = withDefaults(defineProps<{
  rule: Rule
  providers: ProviderDescriptor[]
  endpoint: string
  limit?: number
}>(), {
  limit: 50,
})

const emit = defineEmits<{
  'state-change': [state: 'idle' | 'loading' | 'current' | 'stale' | 'error']
}>()

const { t } = useI18n()
const previewApi = useApi()
const items = ref<RulePreviewPage['items']>([])
const matchedCount = ref<number | null>(null)
const nextCursor = ref('')
const pageIndex = ref(0)
const pendingRequests = ref(0)
const pageCursors = ref<string[]>([''])
const stale = ref(false)
const failed = ref(false)
let requestVersion = 0
let debounceTimer: ReturnType<typeof setTimeout> | undefined

const providerSlugs = computed(() => new Set(props.providers.map((provider) => provider.slug)))
const valid = computed(() => validateRule(props.rule, providerSlugs.value).length === 0)
const hasResults = computed(() => matchedCount.value !== null)
const state = computed<'idle' | 'loading' | 'current' | 'stale' | 'error'>(() => {
  if (pendingRequests.value > 0) return 'loading'
  if (failed.value) return 'error'
  if (stale.value) return 'stale'
  if (hasResults.value) return 'current'
  return 'idle'
})
const statusText = computed(() => {
  if (pendingRequests.value > 0) return t('manage.policy.rule.impactLoading')
  if (stale.value || failed.value) return t('manage.policy.rule.impactStale')
  if (hasResults.value) return t('manage.policy.rule.impactCurrent')
  return valid.value ? t('manage.policy.rule.impactWaiting') : t('manage.policy.rule.impactUnavailable')
})

function requestBody(rule: Rule, cursor: string) {
  const canonical = cloneRule(rule)
  return {
    version: canonical.version,
    condition: canonical.condition,
    cursor,
    limit: props.limit,
  }
}

async function loadPage(cursor: string, nextPageIndex: number, version = ++requestVersion): Promise<void> {
  const rule = cloneRule(props.rule)
  failed.value = false
  pendingRequests.value += 1
  emit('state-change', 'loading')
  const operation = () => api.post<RulePreviewPage>(props.endpoint, requestBody(rule, cursor))
  let page: RulePreviewPage | undefined
  try {
    page = previewApi.busy.value ? await operation() : await previewApi.run(operation)
  } catch {
    page = undefined
  } finally {
    pendingRequests.value -= 1
  }
  if (version !== requestVersion) return
  if (!page) {
    failed.value = true
    stale.value = true
    emit('state-change', 'error')
    return
  }
  items.value = page.items
  matchedCount.value = page.matchedCount
  nextCursor.value = page.nextCursor
  pageIndex.value = nextPageIndex
  pageCursors.value[nextPageIndex] = cursor
  stale.value = false
  failed.value = false
  emit('state-change', 'current')
}

function schedulePreview(): void {
  if (debounceTimer !== undefined) clearTimeout(debounceTimer)
  const version = ++requestVersion
  if (!valid.value) {
    stale.value = hasResults.value
    failed.value = false
    emit('state-change', stale.value ? 'stale' : 'idle')
    return
  }
  if (hasResults.value) stale.value = true
  debounceTimer = setTimeout(() => {
    debounceTimer = undefined
    pageCursors.value = ['']
    void loadPage('', 0, version)
  }, 300)
}

function retry(): void {
  if (!valid.value) return
  if (debounceTimer !== undefined) clearTimeout(debounceTimer)
  debounceTimer = undefined
  const cursor = pageCursors.value[pageIndex.value] ?? ''
  void loadPage(cursor, pageIndex.value)
}

function nextPage(): void {
  if (!nextCursor.value) return
  const nextIndex = pageIndex.value + 1
  pageCursors.value[nextIndex] = nextCursor.value
  void loadPage(nextCursor.value, nextIndex)
}

function previousPage(): void {
  if (pageIndex.value <= 0) return
  const previousIndex = pageIndex.value - 1
  void loadPage(pageCursors.value[previousIndex] ?? '', previousIndex)
}

function accountName(item: RulePreviewPage['items'][number]): string {
  return item.account.displayName || item.account.username
}

watch(() => [props.rule, props.providers, props.endpoint, props.limit], schedulePreview, { deep: true, immediate: true })
watch(state, (next) => emit('state-change', next), { immediate: true })

onBeforeUnmount(() => {
  requestVersion += 1
  if (debounceTimer !== undefined) clearTimeout(debounceTimer)
})
</script>

<template>
  <section
    class="rounded-lg border border-border bg-sunken p-4"
    :data-state="state === 'error' ? 'stale' : state"
    data-test="rule-impact-preview"
    aria-labelledby="rule-impact-heading"
  >
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0">
        <h3 id="rule-impact-heading" class="text-sm font-semibold text-ink">
          {{ t('manage.policy.rule.impactTitle') }}
        </h3>
        <p v-if="matchedCount !== null" class="mt-1 text-sm text-ink" data-test="impact-count">
          {{ t('manage.policy.rule.impactMatchCount', { count: matchedCount }) }}
        </p>
      </div>
      <StatusBadge v-if="stale || failed" variant="caution">
        {{ t('manage.policy.rule.outOfDate') }}
      </StatusBadge>
    </div>

    <p
      role="status"
      aria-live="polite"
      aria-atomic="true"
      class="mt-2 text-sm text-muted"
      data-test="impact-status"
    >
      {{ statusText }}
    </p>

    <Button
      v-if="failed"
      type="button"
      variant="outline"
      size="sm"
      class="mt-3 shadow-none"
      data-test="impact-retry"
      @click="retry"
    >
      <RotateCcw class="size-4" aria-hidden="true" />
      {{ t('manage.policy.rule.retryPreview') }}
    </Button>

    <p v-if="!hasResults && pendingRequests === 0 && !failed" class="mt-4 text-sm text-muted">
      {{ valid ? t('manage.policy.rule.impactEmptyWaiting') : t('manage.policy.rule.impactFixRule') }}
    </p>

    <p
      v-else-if="hasResults && items.length === 0"
      class="mt-4 text-sm text-muted"
      data-test="impact-empty"
    >
      {{ t('manage.policy.rule.previewEmpty') }}
    </p>

    <ul v-else-if="items.length" class="mt-4 divide-y divide-border border-y border-border">
      <li
        v-for="item in items"
        :key="item.account.id"
        :data-test="`impact-row-${item.account.id}`"
        class="flex flex-col gap-2 py-3 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="min-w-0">
          <p class="truncate text-sm font-medium text-ink">{{ accountName(item) }}</p>
          <p class="truncate font-mono text-xs text-muted">{{ item.account.username }}</p>
        </div>
        <StatusBadge :variant="item.matched ? 'success' : 'neutral'">
          {{ item.matched ? t('manage.policy.rule.matches') : t('manage.policy.rule.doesNotMatch') }}
        </StatusBadge>
      </li>
    </ul>

    <PaginationControls
      class="mt-4"
      :page-index="pageIndex"
      :has-more="nextCursor !== ''"
      :busy="pendingRequests > 0"
      :has-items="items.length > 0"
      @next="nextPage"
      @previous="previousPage"
    />
  </section>
</template>
