<script setup lang="ts">
/** AdminGroupsView (/admin/groups) — table of groups; inline create; row → detail. */
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import StatusMessage from '@/components/custom/StatusMessage.vue'
import { useRouter } from 'vue-router'
import { api } from '@/lib/api'
import { useApi } from '@/composables/useApi'
import { useCursorPage } from '@/composables/useCursorPage'
import { type Page, buildPagePath } from '@/lib/pagination'
import { useTransientFlag } from '@/composables/useTransientFlag'
import { withSudo } from '@/lib/sudo'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import TableSkeleton from '@/components/custom/TableSkeleton.vue'
import FormSection from '@/components/custom/FormSection.vue'
import SettingRow from '@/components/custom/SettingRow.vue'
import EmptyState from '@/components/custom/EmptyState.vue'
import ErrorPanel from '@/components/custom/ErrorPanel.vue'
import PaginationControls from '@/components/custom/PaginationControls.vue'
import { UsersRound } from 'lucide-vue-next'

interface GroupView {
  id: number
  slug: string
  displayName: string
  description?: string
  exposedToDownstream: boolean
  memberCount: number
  createdAt: string
}

const { t } = useI18n()
const router = useRouter()
const { busy, run, error, clear } = useApi()
const { flag: created, trigger: triggerCreated } = useTransientFlag()

const page = useCursorPage<GroupView>((cursor) =>
  api.get<Page<GroupView>>(buildPagePath('/api/prohibitorum/groups', { cursor })),
)
const rows = page.items
const createOpen = ref(false)
const displayError = computed(() => page.error.value ?? error.value)
function clearError(): void { page.clear(); clear() }

// Create form state
const slug = ref('')
const displayName = ref('')
const description = ref('')
const exposedToDownstream = ref(false)
const slugError = ref('')

const SLUG_RE = /^[a-z0-9](-?[a-z0-9])*$/

function validateSlug(s: string): string {
  if (!s) return t('admin.groups.slugInvalid')
  if (!SLUG_RE.test(s)) return t('admin.groups.slugInvalid')
  return ''
}

function onSlugInput(): void {
  slugError.value = validateSlug(slug.value)
}

function go(id: number): void { router.push(`/admin/groups/${id}`) }

async function create(): Promise<void> {
  const err = validateSlug(slug.value)
  if (err) { slugError.value = err; return }
  slugError.value = ''
  const res = await run(() => withSudo(() => api.post<GroupView>('/api/prohibitorum/groups', {
    slug: slug.value,
    displayName: displayName.value,
    description: description.value || undefined,
    exposedToDownstream: exposedToDownstream.value,
  })))
  if (res) {
    createOpen.value = false
    triggerCreated()
    await page.reload()
  }
}

function openCreate(): void {
  slug.value = ''
  displayName.value = ''
  description.value = ''
  exposedToDownstream.value = false
  slugError.value = ''
  createOpen.value = true
}
</script>
<template>
  <div class="flex max-w-4xl flex-col gap-6">
    <div class="flex items-center justify-between gap-4">
      <h1 class="text-2xl font-semibold tracking-tight text-ink">{{ t('admin.groups.title') }}</h1>
      <Button type="button" data-test="create" @click="openCreate">{{ t('admin.groups.create') }}</Button>
    </div>
    <ErrorPanel :error="displayError" @dismiss="clearError" :is-admin="true" />
    <StatusMessage :show="created">{{ t('admin.groups.created') }}</StatusMessage>

    <Card v-if="createOpen">
      <CardHeader><CardTitle>{{ t('admin.groups.createTitle') }}</CardTitle></CardHeader>
      <CardContent class="flex flex-col gap-4 py-4">
        <FormSection :title="t('admin.groups.sectionBasics')">
          <div class="flex flex-col gap-1.5">
            <Label for="slug">{{ t('admin.groups.slug') }}</Label>
            <Input id="slug" name="slug" v-model="slug" autocomplete="off" data-test="create-slug" @input="onSlugInput" />
            <p v-if="slugError" class="text-xs text-destructive" role="alert">{{ slugError }}</p>
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="displayName">{{ t('admin.groups.displayName') }}</Label>
            <Input id="displayName" name="displayName" v-model="displayName" data-test="create-displayName" />
          </div>
          <div class="flex flex-col gap-1.5">
            <Label for="description">{{ t('admin.groups.description') }}</Label>
            <Input id="description" name="description" v-model="description" data-test="create-description" />
          </div>
        </FormSection>
        <FormSection :title="t('admin.groups.sectionOptions')">
          <SettingRow :label="t('admin.groups.exposed')" :description="t('admin.groups.exposedHint')" for="exposed">
            <Switch id="exposed" name="exposed" data-test="create-exposed" v-model="exposedToDownstream" />
          </SettingRow>
        </FormSection>
        <div class="flex gap-2">
          <Button type="button" :disabled="busy" data-test="create-confirm" @click="create">{{ t('admin.groups.create') }}</Button>
          <Button type="button" variant="outline" :disabled="busy" data-test="create-cancel" @click="createOpen = false">{{ t('common.cancel') }}</Button>
        </div>
      </CardContent>
    </Card>

    <TableSkeleton v-if="page.busy.value && !rows.length" :rows="5" :cols="3" />
    <Table v-else-if="rows.length">
      <TableHeader>
        <TableRow>
          <TableHead>{{ t('admin.groups.colName') }} · {{ t('admin.groups.slug') }}</TableHead>
          <TableHead>{{ t('admin.groups.colMembers') }}</TableHead>
          <TableHead>{{ t('admin.groups.colExposed') }}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        <TableRow v-for="g in rows" :key="g.id" class="cursor-pointer" tabindex="0"
                  :data-test="`group-row-${g.id}`"
                  @click="go(g.id)" @keydown.enter="go(g.id)" @keydown.space.prevent="go(g.id)">
          <TableCell>
            <div class="flex min-w-0 flex-col">
              <span class="truncate font-medium text-ink">{{ g.displayName }}</span>
              <span class="truncate text-muted">{{ g.slug }}</span>
            </div>
          </TableCell>
          <TableCell class="text-muted">{{ g.memberCount }}</TableCell>
          <TableCell>
            <StatusBadge :variant="g.exposedToDownstream ? 'success' : 'neutral'">
              {{ g.exposedToDownstream ? t('admin.groups.exposedYes') : t('admin.groups.exposedNo') }}
            </StatusBadge>
          </TableCell>
        </TableRow>
      </TableBody>
    </Table>
    <EmptyState v-else-if="!displayError && !createOpen" :icon="UsersRound" :title="t('admin.groups.empty')" />
    <PaginationControls
      v-if="rows.length || page.pageIndex.value > 0"
      :page-index="page.pageIndex.value"
      :has-more="page.hasMore.value"
      :busy="page.busy.value"
      :has-items="rows.length > 0"
      @next="page.next"
      @previous="page.previous"
    />
  </div>
</template>
