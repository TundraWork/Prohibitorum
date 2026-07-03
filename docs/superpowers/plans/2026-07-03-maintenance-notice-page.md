# Maintenance-mode Notice Page + Admin Login Link — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers-extended-cc:subagent-driven-development (recommended) or superpowers-extended-cc:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** During maintenance, funnel all non-admin visitors to the dedicated `/maintenance` notice page (no login form) and give that page a deliberate "Administrator sign-in" link to the normal login form via `/login?admin=1`.

**Architecture:** Front-end only. The router guard (`installGuard`) confines non-admins to `/maintenance`, `/logout`, and `/login?admin=1` while maintenance is on; everything else redirects to `/maintenance`. `MaintenanceView` gains a quiet admin-login link (shown to unauthenticated visitors, alongside the existing sign-out for authenticated ones). The backend already enforces admin-only sign-in during maintenance, so `?admin=1` is a UX affordance, not a security boundary. No backend changes.

**Tech Stack:** Vue 3 + vue-router + Pinia, Vitest, vue-tsc, Tailwind v4 / shadcn-vue. Dashboard embedded into Go via `pkg/webui` `//go:embed all:dist`.

**User decisions (already made):**
- Admin entry mechanism: "Query flag on /login" (`/login?admin=1`).
- Admin login page: "Reuse current login page" (existing `LoginView`, existing maintenance banner).
- Optional `return_to` preservation through the maintenance bounce: deferred (out of scope).

**Spec:** `docs/superpowers/specs/2026-07-03-maintenance-notice-page-design.md`

---

## File structure

- `dashboard/src/router/index.ts` — rewrite the maintenance block inside `installGuard` (Task 1).
- `dashboard/src/router/guard.test.ts` — add maintenance-mode guard cases (Task 1).
- `dashboard/src/pages/MaintenanceView.vue` — add the admin-sign-in link (Task 2).
- `dashboard/src/pages/MaintenanceView.test.ts` — add admin-link cases (Task 2).
- `dashboard/src/locales/en.ts`, `dashboard/src/locales/zh.ts` — add `maintenance.adminSignIn` (Task 2).
- `dashboard/dist/**` — rebuilt embedded assets, committed at the done-gate (Task 3).

---

### Task 1: Router guard confines non-admins during maintenance

**Goal:** When maintenance is on, redirect every non-admin (authenticated or not) to `/maintenance`, except when navigating to `logout` or `login` with the `admin` query flag.

**Files:**
- Modify: `dashboard/src/router/index.ts` (the maintenance block in `installGuard`, currently ~lines 154–184)
- Test: `dashboard/src/router/guard.test.ts`

**Acceptance Criteria:**
- [ ] Maintenance on + unauthenticated navigating to `/login` (no flag) → redirected to `/maintenance`.
- [ ] Maintenance on + navigating to `/login?admin=1` → allowed; stays on `login` with `query.admin === '1'`.
- [ ] Maintenance on + authenticated non-admin navigating to an app route → redirected to `/maintenance`.
- [ ] Maintenance on + authenticated admin → passes through (no maintenance redirect).
- [ ] Maintenance off + on `/maintenance` → redirected to `/login` (unchanged behavior).
- [ ] Existing guard tests (`requiresAdmin`, `3c admin routes`) still pass.

**Verify:** `cd dashboard && npx vitest run src/router/guard.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Write the failing tests.** Append to `dashboard/src/router/guard.test.ts` (after the existing `describe` blocks). Note the per-path `api.get` mock — the guard fetches `/api/prohibitorum/config` (branding) and `/api/prohibitorum/me` (auth):

```ts
import { createMemoryHistory as _mh } from 'vue-router' // (already imported above; keep single import)

function makeMaintRouter() {
  const r = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/login', name: 'login', component: stub, meta: { public: true } },
      { path: '/maintenance', name: 'maintenance', component: stub, meta: { public: true } },
      { path: '/logout', name: 'logout', component: stub, meta: { public: true } },
      { path: '/app', name: 'test-app', component: stub, meta: { requiresAuth: true } },
    ],
  })
  installGuard(r)
  return r
}

// Route api.get by path: config carries maintenanceMode; me is the session (or 401).
function mockApi(maintenance: boolean, me: { role: string } | null) {
  get.mockImplementation(((path: string) => {
    if (path === '/api/prohibitorum/config') {
      return Promise.resolve({ maintenanceMode: maintenance, maintenanceMessage: '' })
    }
    if (path === '/api/prohibitorum/me') {
      return me
        ? Promise.resolve({ id: 1, username: 'u', displayName: 'U', role: me.role })
        : Promise.reject({ code: 'unauthorized' })
    }
    return Promise.resolve({})
  }) as any)
}

describe('router guard (maintenance mode)', () => {
  it('redirects an unauthenticated visitor from /login to /maintenance', async () => {
    mockApi(true, null)
    const r = makeMaintRouter()
    await r.push('/login'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('maintenance')
  })

  it('allows /login?admin=1 through during maintenance', async () => {
    mockApi(true, null)
    const r = makeMaintRouter()
    await r.push('/login?admin=1'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('login')
    expect(r.currentRoute.value.query.admin).toBe('1')
  })

  it('redirects an authenticated non-admin from an app route to /maintenance', async () => {
    mockApi(true, { role: 'user' })
    const r = makeMaintRouter()
    await r.push('/app'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('maintenance')
  })

  it('lets an admin through during maintenance', async () => {
    mockApi(true, { role: 'admin' })
    const r = makeMaintRouter()
    await r.push('/app'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('test-app')
  })

  it('redirects off /maintenance to /login when maintenance is off', async () => {
    mockApi(false, null)
    const r = makeMaintRouter()
    await r.push('/maintenance'); await r.isReady()
    expect(r.currentRoute.value.name).toBe('login')
  })
})
```

Remove the placeholder `import { createMemoryHistory as _mh }` line above — `createMemoryHistory` is already imported at the top of the file (line 2). It is shown only to flag the dependency; do not add a duplicate import.

- [ ] **Step 2: Run tests to verify they fail.**

Run: `cd dashboard && npx vitest run src/router/guard.test.ts`
Expected: the 5 new maintenance cases FAIL (current guard lets unauthenticated `/login` through and does not confine non-admins), while the pre-existing cases pass.

- [ ] **Step 3: Rewrite the maintenance block in `installGuard`.** In `dashboard/src/router/index.ts`, replace the inner `if (branding.maintenanceMode) { … } else { … }` block. Find this current code:

```ts
        if (branding.maintenanceMode) {
          // Check if the visitor is an authenticated admin — admins bypass maintenance.
          const { useAuthStore: useAuth } = await import('@/stores/auth')
          const auth = useAuth(pinia)
          // GET /me is allowlisted by the backend during maintenance.
          try { await auth.ensureLoaded() } catch { /* treat as unauthenticated */ }

          if (auth.me && !auth.isAdmin) {
            // Authenticated non-admin: redirect to maintenance (allow logout).
            if (to.name !== 'maintenance' && to.name !== 'logout') {
              return { name: 'maintenance' }
            }
            return true
          }
          // Unauthenticated users may still reach /login (so admins can sign in).
          // Admins fall through to normal flow below.
        } else {
          // Maintenance is off — don't strand anyone on the maintenance page.
          if (to.name === 'maintenance') return { name: 'login' }
        }
```

Replace it with:

```ts
        if (branding.maintenanceMode) {
          // Admins bypass maintenance entirely; everyone else — unauthenticated
          // visitors AND authenticated non-admins — is confined to the notice
          // page, sign-out, and the deliberate admin-login entry (/login?admin=1).
          const { useAuthStore: useAuth } = await import('@/stores/auth')
          const auth = useAuth(pinia)
          // GET /me is allowlisted by the backend during maintenance.
          try { await auth.ensureLoaded() } catch { /* treat as unauthenticated */ }

          if (!(auth.me && auth.isAdmin)) {
            const allowed =
              to.name === 'maintenance' ||
              to.name === 'logout' ||
              (to.name === 'login' && to.query.admin != null)
            if (!allowed) return { name: 'maintenance' }
            return true
          }
          // Admin: fall through to the normal flow below.
        } else {
          // Maintenance is off — don't strand anyone on the maintenance page.
          if (to.name === 'maintenance') return { name: 'login' }
        }
```

- [ ] **Step 4: Run tests to verify they pass.**

Run: `cd dashboard && npx vitest run src/router/guard.test.ts`
Expected: PASS (all maintenance cases + pre-existing cases).

- [ ] **Step 5: Commit.**

```bash
git add dashboard/src/router/index.ts dashboard/src/router/guard.test.ts
git commit -m "feat(router): confine non-admins to /maintenance during maintenance mode"
```

---

### Task 2: MaintenanceView admin-sign-in link + i18n

**Goal:** Add a quiet "Administrator sign-in" link on `MaintenanceView` (shown to unauthenticated visitors) that points to `/login?admin=1`, with the label localized in `en` and `zh`.

**Files:**
- Modify: `dashboard/src/pages/MaintenanceView.vue` (footer links)
- Modify: `dashboard/src/locales/en.ts` (`maintenance` block, ~line 781)
- Modify: `dashboard/src/locales/zh.ts` (`maintenance` block, ~line 777)
- Test: `dashboard/src/pages/MaintenanceView.test.ts`

**Acceptance Criteria:**
- [ ] Unauthenticated → an anchor with `href="/login?admin=1"` and text `en.maintenance.adminSignIn` is rendered.
- [ ] Authenticated → no `/login?admin=1` link; the existing `/logout` sign-out link is rendered.
- [ ] `maintenance.adminSignIn` exists in both `en.ts` and `zh.ts`.
- [ ] No apostrophe/`@` regressions in `en.ts` (grep clean).

**Verify:** `cd dashboard && npx vitest run src/pages/MaintenanceView.test.ts` → all pass.

**Steps:**

- [ ] **Step 1: Add the i18n key to `en.ts`.** In `dashboard/src/locales/en.ts`, the `maintenance` block currently reads:

```ts
  maintenance: {
    heading: "We'll be right back",
    body: "We're making some improvements to {instance}. We'll be back online shortly — thanks for your patience.",
    noteLabel: 'A note from the team',
    retry: 'Try again',
    signOut: 'Sign out',
    adminBanner: 'Maintenance mode is on — non-admin users are locked out.',
    adminBannerAction: 'Manage',
  },
```

Add `adminSignIn` after `signOut`:

```ts
    signOut: 'Sign out',
    adminSignIn: 'Administrator sign-in',
```

- [ ] **Step 2: Add the i18n key to `zh.ts`.** In `dashboard/src/locales/zh.ts`, the `maintenance` block currently reads:

```ts
  maintenance: {
    heading: '我们马上回来',
    body: '我们正在对 {instance} 进行改进，很快就会恢复上线——感谢你的耐心等待。',
    noteLabel: '来自团队的说明',
    retry: '重试',
    signOut: '退出登录',
    adminBanner: '维护模式已开启——非管理员用户当前无法访问。',
    adminBannerAction: '管理',
  },
```

Add `adminSignIn` after `signOut`:

```ts
    signOut: '退出登录',
    adminSignIn: '管理员登录',
```

- [ ] **Step 3: Add the admin-sign-in link to `MaintenanceView.vue`.** In `dashboard/src/pages/MaintenanceView.vue`, the footer currently ends with:

```html
      <Button variant="outline" class="w-full" @click="reload">
        {{ t('maintenance.retry') }}
      </Button>

      <!-- Sign-out link for authenticated users who are stuck here -->
      <RouterLink
        v-if="hasSession"
        to="/logout"
        class="text-xs text-muted underline underline-offset-4 hover:text-ink"
      >
        {{ t('maintenance.signOut') }}
      </RouterLink>
    </div>
  </CenteredLayout>
</template>
```

Replace the sign-out `RouterLink` with the sign-out (authenticated) + admin-sign-in (unauthenticated) pair:

```html
      <Button variant="outline" class="w-full" @click="reload">
        {{ t('maintenance.retry') }}
      </Button>

      <!-- Authenticated users who are stuck here can sign out; unauthenticated
           visitors get the deliberate admin-login entry (the form is otherwise
           unreachable during maintenance). -->
      <RouterLink
        v-if="hasSession"
        to="/logout"
        class="text-xs text-muted underline underline-offset-4 hover:text-ink"
      >
        {{ t('maintenance.signOut') }}
      </RouterLink>
      <RouterLink
        v-else
        to="/login?admin=1"
        class="text-xs text-muted underline underline-offset-4 hover:text-ink"
      >
        {{ t('maintenance.adminSignIn') }}
      </RouterLink>
    </div>
  </CenteredLayout>
</template>
```

- [ ] **Step 4: Add the tests.** Append to the `describe('MaintenanceView', …)` block in `dashboard/src/pages/MaintenanceView.test.ts`:

```ts
  it('shows the admin-sign-in link when unauthenticated', async () => {
    authState.me = null
    const wrapper = mountView()
    await flushPromises()
    const link = wrapper.findAll('a').find(l => l.attributes('href') === '/login?admin=1')
    expect(link).toBeDefined()
    expect(link!.text()).toBe(en.maintenance.adminSignIn)
  })

  it('does NOT show the admin-sign-in link when authenticated', async () => {
    authState.me = { id: 1, username: 'alex' }
    const wrapper = mountView()
    await flushPromises()
    const link = wrapper.findAll('a').find(l => l.attributes('href') === '/login?admin=1')
    expect(link).toBeUndefined()
  })
```

- [ ] **Step 5: Run the component tests.**

Run: `cd dashboard && npx vitest run src/pages/MaintenanceView.test.ts`
Expected: PASS (new cases + existing heading/body/note/sign-out cases).

- [ ] **Step 6: Grep-verify `en.ts` for the apostrophe hazard.**

Run: `grep -n "adminSignIn" dashboard/src/locales/en.ts dashboard/src/locales/zh.ts`
Expected: `en.ts` shows `adminSignIn: 'Administrator sign-in',` with straight ASCII quotes (`'`), and `zh.ts` shows `adminSignIn: '管理员登录',`. If Edit flipped any `'` to a curly `‘`/`’`, fix it back to ASCII.

- [ ] **Step 7: Commit.**

```bash
git add dashboard/src/pages/MaintenanceView.vue dashboard/src/pages/MaintenanceView.test.ts dashboard/src/locales/en.ts dashboard/src/locales/zh.ts
git commit -m "feat(maintenance): add admin sign-in link to the maintenance notice page"
```

---

### Task 3: Done-gate — full gate + rebuild & commit dist

**Goal:** Run the project's green gate and commit the rebuilt embedded `dist` so the Go binary serves the updated SPA.

**Files:**
- Modify: `dashboard/dist/**` (regenerated by `npm run build`)

**Acceptance Criteria:**
- [ ] Full vitest suite passes (existing count + the 7 new cases from Tasks 1–2).
- [ ] `npm run build` (which runs `vue-tsc -b` then `vite build`) completes with no type errors and regenerates `dist`.
- [ ] `node scripts/check-contrast.mjs` passes (no color changes → still 31/31).
- [ ] `go build -tags nodynamic ./...` and `go vet ./...` succeed (no Go changes; sanity only).
- [ ] `dashboard/dist` is rebuilt and committed.

**Verify:** the four commands below each exit 0; `git status` shows `dashboard/dist` staged before the commit.

**Steps:**

- [ ] **Step 1: Full unit suite.**

Run: `cd dashboard && npx vitest run`
Expected: all tests pass (prior green count + 7 new).

- [ ] **Step 2: Typecheck + build dist.**

Run: `cd dashboard && npm run build`
Expected: `vue-tsc -b` reports no errors; `vite build` writes hashed assets into `dashboard/dist`.

- [ ] **Step 3: Contrast gate.**

Run: `cd dashboard && node scripts/check-contrast.mjs`
Expected: all pairs pass (e.g. `31/31`).

- [ ] **Step 4: Go sanity (no Go changes).**

Run: `go build -tags nodynamic ./... && go vet ./...`
Expected: both exit 0 with no output.

- [ ] **Step 5: Commit the rebuilt dist.**

```bash
git add dashboard/dist
git commit -m "build(dashboard): rebuild dist for maintenance notice page"
```

If `git status` shows no `dist` changes (Vite produced identical output), skip the commit and note it.

---

## Self-review

- **Spec coverage:** guard confinement of non-admins (spec §1) → Task 1; unauthenticated `/login` → `/maintenance` (spec §1) → Task 1; admin bypass + `/login?admin=1` allowance (spec §1) → Task 1; MaintenanceView admin-sign-in link + hasSession split (spec §2) → Task 2; admin page = existing LoginView, no change (spec §3) → no task needed (intentional); i18n key en+zh (spec §4) → Task 2; guard + MaintenanceView tests (spec Testing) → Tasks 1–2; gate + dist rebuild (spec Testing) → Task 3. `return_to` preservation is an explicit non-goal → no task. All covered.
- **Placeholder scan:** none — every code step shows full code; the one `import … as _mh` line is explicitly flagged as illustrative and instructed to be removed.
- **Type consistency:** guard uses `auth.me`, `auth.isAdmin` (both exported by the auth store), `to.query.admin`, `to.name`; link uses `to="/login?admin=1"` string form (parsed to `query.admin === '1'`, matching the guard check); i18n key `maintenance.adminSignIn` used identically in the template and both tests.
