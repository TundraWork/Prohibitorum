<script setup lang="ts">
// Dev console — a chrome://-style hub for manually exercising every page/flow.
// Dev-only (see lib/devMode); English-only by intent (internal tooling, not a
// product surface, so it is deliberately NOT wired into vue-i18n).
import { ref, onMounted } from 'vue'
import { api } from '../lib/api'
import { useSessionStore } from '../stores/session'
import { isDevMode } from '../lib/devMode'

const dev = isDevMode()
const session = useSessionStore()
const origin = typeof window !== 'undefined' ? window.location.origin : ''

const routeGroups = [
  {
    group: 'Public',
    items: [
      { to: '/login', label: 'Login', note: 'passkey / password→TOTP' },
      { to: '/logout', label: 'Logout' },
      { to: '/error?code=server_error', label: 'Error page', note: 'sample code' },
    ],
  },
  {
    group: 'User · requires session',
    items: [
      { to: '/', label: 'Profile' },
      { to: '/sessions', label: 'Sessions' },
      { to: '/credentials', label: 'Passkeys' },
    ],
  },
  {
    group: 'Admin · requires admin',
    items: [
      { to: '/admin/accounts', label: 'Accounts' },
      { to: '/admin/invitations', label: 'Invitations' },
    ],
  },
]

// Backend endpoints worth poking at directly (open in a new tab).
const apiLinks = [
  { href: '/api/prohibitorum/me', label: 'GET /me' },
  { href: '/api/prohibitorum/auth/federation', label: 'GET /auth/federation' },
  { href: '/.well-known/openid-configuration', label: 'OIDC discovery' },
  { href: '/oauth/jwks', label: 'JWKS' },
  { href: '/saml/metadata', label: 'SAML metadata' },
]

const busy = ref(false)
const error = ref('')
const inviteUrl = ref('')
const invitePath = ref('')

async function refreshSession() {
  await session.fetchMe()
}

// Mint a fresh invitation (intent=invite, role=user) and surface its /enroll URL
// so the enrollment ceremony is one click away — the standalone equivalent of
// `prohibitorum enroll-admin`, but for an already-signed-in admin.
async function mintInvite() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  inviteUrl.value = ''
  invitePath.value = ''
  try {
    const r = await api.post<{ url: string }>('/api/prohibitorum/invitations', { role: 'user' })
    inviteUrl.value = r.url
    try { invitePath.value = new URL(r.url).pathname } catch { invitePath.value = '' }
  } catch (e: any) {
    error.value = e?.message ?? 'request failed'
  } finally {
    busy.value = false
  }
}

onMounted(() => { if (dev) void refreshSession() })
</script>

<template>
  <div v-if="!dev" class="min-h-screen flex items-center justify-center p-6 text-muted">
    The dev console is only available in development.
  </div>

  <div v-else class="min-h-screen bg-default p-6">
    <div class="mx-auto max-w-3xl space-y-6">
      <header class="flex items-center gap-3">
        <h1 class="text-xl font-semibold">Dev console</h1>
        <UBadge color="warning" variant="subtle">dev-only</UBadge>
        <span class="text-xs text-muted ml-auto font-mono">{{ origin }}</span>
      </header>
      <p class="text-sm text-muted">
        Quick links to every page and flow for manual testing. Hidden in real deployments
        (served only on loopback). Actions here use the same APIs/permissions as the app.
      </p>

      <!-- Session status -->
      <UCard>
        <template #header><h2 class="font-medium">Session</h2></template>
        <div class="text-sm flex flex-wrap items-center gap-x-6 gap-y-2">
          <template v-if="session.me">
            <span>signed in as <strong>{{ session.me.username }}</strong></span>
            <UBadge size="sm" :color="session.isAdmin ? 'primary' : 'neutral'">{{ session.me.role }}</UBadge>
          </template>
          <span v-else class="text-muted">not signed in</span>
          <span class="ml-auto flex gap-2">
            <UButton type="button" size="xs" variant="soft" :disabled="busy" @click="refreshSession">Refresh</UButton>
            <RouterLink to="/login"><UButton type="button" size="xs" variant="soft">Login</UButton></RouterLink>
            <RouterLink to="/logout"><UButton type="button" size="xs" color="neutral" variant="ghost">Logout</UButton></RouterLink>
          </span>
        </div>
      </UCard>

      <!-- Pages -->
      <UCard>
        <template #header><h2 class="font-medium">Pages</h2></template>
        <div class="grid sm:grid-cols-3 gap-4">
          <div v-for="g in routeGroups" :key="g.group">
            <div class="text-xs uppercase tracking-wide text-muted mb-2">{{ g.group }}</div>
            <ul class="space-y-1">
              <li v-for="it in g.items" :key="it.to">
                <RouterLink :to="it.to" class="text-primary hover:underline text-sm">{{ it.label }}</RouterLink>
                <span v-if="it.note" class="text-xs text-muted"> — {{ it.note }}</span>
              </li>
            </ul>
          </div>
        </div>
        <p class="text-xs text-muted mt-3">
          Visiting a guarded page while signed out bounces to <code>/login?return_to=…</code>;
          a non-admin hitting an admin page bounces to <code>/</code>.
        </p>
      </UCard>

      <!-- Enrollment flow -->
      <UCard>
        <template #header><h2 class="font-medium">Enrollment</h2></template>
        <p v-if="error" role="alert" aria-live="polite" class="text-error text-sm mb-2">{{ error }}</p>
        <template v-if="session.isAdmin">
          <UButton type="button" size="sm" :loading="busy" :disabled="busy" @click="mintInvite">
            Mint invitation &amp; get enroll link
          </UButton>
          <div v-if="inviteUrl" class="mt-3 text-sm space-y-1">
            <div>
              In-app:
              <RouterLink v-if="invitePath" :to="invitePath" class="text-primary hover:underline font-mono">{{ invitePath }}</RouterLink>
            </div>
            <div class="text-xs text-muted break-all font-mono">{{ inviteUrl }}</div>
          </div>
        </template>
        <p v-else class="text-sm text-muted">
          Sign in as an admin to mint an enroll link here, or bootstrap the first admin with
          <code>go run ./cmd/prohibitorum enroll-admin</code> and open the printed <code>/enroll/&lt;token&gt;</code>.
        </p>
      </UCard>

      <!-- Raw API endpoints -->
      <UCard>
        <template #header><h2 class="font-medium">API endpoints</h2></template>
        <ul class="flex flex-wrap gap-x-6 gap-y-1 text-sm">
          <li v-for="a in apiLinks" :key="a.href">
            <a :href="a.href" target="_blank" rel="noopener" class="text-primary hover:underline font-mono">{{ a.label }}</a>
          </li>
        </ul>
      </UCard>

      <!-- Flows needing external parties -->
      <UCard>
        <template #header><h2 class="font-medium">Needs external parties (not standalone)</h2></template>
        <ul class="text-sm text-muted list-disc pl-5 space-y-1">
          <li><strong>Consent</strong> (<code>/consent</code>) is reached when a downstream OIDC client redirects to <code>/oauth/authorize</code>; register a client and start an authorize request to drive it.</li>
          <li><strong>Federation login</strong> needs a live upstream OIDC provider; it is exercised by the smoke suite (in-process mock OP), not the standalone dev server, so no federation buttons appear here.</li>
        </ul>
      </UCard>
    </div>
  </div>
</template>
