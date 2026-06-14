<script setup lang="ts">
/**
 * NavUser - sidebar footer account control. A dropdown over the vendored
 * DropdownMenu: identity header, edit-display-name, sign out. The edit dialog
 * is a SIBLING (not nested in the menu) and opens on nextTick after select,
 * avoiding Reka's menu->dialog focus / lingering-pointer-events bug.
 */
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { ChevronsUpDown, LogOut, Pencil } from 'lucide-vue-next'
import { useAuthStore } from '@/stores/auth'
import { SidebarMenu, SidebarMenuItem, SidebarMenuButton } from '@/components/ui/sidebar'
import { Skeleton } from '@/components/ui/skeleton'
import {
  DropdownMenu, DropdownMenuTrigger, DropdownMenuContent,
  DropdownMenuLabel, DropdownMenuItem, DropdownMenuSeparator,
} from '@/components/ui/dropdown-menu'
import StatusBadge from '@/components/custom/StatusBadge.vue'
import UserAvatar from '@/components/custom/UserAvatar.vue'
import EditProfileDialog from '@/components/custom/EditProfileDialog.vue'

const { t } = useI18n()
const auth = useAuthStore()
const router = useRouter()

const editOpen = ref(false)

let cancelPoll: (() => void) | null = null
onMounted(() => { cancelPoll = auth.pollAvatarUntilSettled() })
onUnmounted(() => { cancelPoll?.(); cancelPoll = null })

// Open the dialog on the next tick so the menu finishes closing / restoring
// focus to the trigger first - prevents Reka's lingering pointer-events:none.
function openEdit(): void { void nextTick(() => { editOpen.value = true }) }
function signOut(): void { void router.push('/logout') }

defineExpose({ openEdit, signOut, editOpen })
</script>

<template>
  <SidebarMenu>
    <SidebarMenuItem>
      <div v-if="!auth.me" class="flex items-center gap-2 p-2">
        <Skeleton class="size-8 rounded-md" />
        <div class="flex flex-1 flex-col gap-1">
          <Skeleton class="h-3.5 w-24" />
          <Skeleton class="h-3 w-12" />
        </div>
      </div>

      <DropdownMenu v-else>
        <DropdownMenuTrigger as-child>
          <SidebarMenuButton
            size="lg"
            data-test="account-trigger"
            :aria-label="t('accountMenu.trigger')"
            class="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
          >
            <UserAvatar :display-name="auth.me.displayName" :username="auth.me.username" :src="auth.me.avatarUrl" :loading="auth.me.avatarPending" />
            <div class="grid flex-1 text-left text-sm leading-tight">
              <span class="truncate font-medium text-ink">{{ auth.me.displayName }}</span>
              <span class="truncate text-xs capitalize text-muted">{{ auth.me.role }}</span>
            </div>
            <ChevronsUpDown class="ml-auto size-4 text-muted" aria-hidden="true" />
          </SidebarMenuButton>
        </DropdownMenuTrigger>

        <DropdownMenuContent
          class="min-w-56"
          side="top"
          align="start"
          :side-offset="4"
        >
          <DropdownMenuLabel class="font-normal">
            <div class="flex items-center gap-2">
              <!-- No :loading here intentionally — the spinner is only on the always-visible trigger avatar, not the dropdown header -->
              <UserAvatar :display-name="auth.me.displayName" :username="auth.me.username" :src="auth.me.avatarUrl" />
              <div class="grid flex-1 text-left text-sm leading-tight">
                <span class="truncate font-medium text-ink">{{ auth.me.displayName }}</span>
                <span class="truncate text-xs text-muted">@{{ auth.me.username }}</span>
              </div>
              <StatusBadge :variant="auth.me.role === 'admin' ? 'caution' : 'neutral'" class="capitalize">
                {{ auth.me.role }}
              </StatusBadge>
            </div>
          </DropdownMenuLabel>
          <DropdownMenuSeparator />
          <DropdownMenuItem data-test="account-edit" @select="openEdit">
            <Pencil />
            <span>{{ t('accountMenu.editProfile') }}</span>
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem data-test="account-signout" @select="signOut">
            <LogOut />
            <span>{{ t('nav.signOut') }}</span>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
    </SidebarMenuItem>
  </SidebarMenu>

  <EditProfileDialog v-model:open="editOpen" />
</template>
