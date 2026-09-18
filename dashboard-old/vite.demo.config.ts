/**
 * Demo Vite config — runs the REAL dashboard (LauncherLayout + MyAppsView +
 * AppTile + reka-ui menus/tooltips/dialog) with NO backend, by answering the
 * launcher's API calls from in-memory fixtures via a dev-server middleware.
 *
 * Nothing in src/ is touched: the app makes its normal relative-path fetches,
 * and the middleware below intercepts them before Vite's SPA fallback. This
 * shows the end-user "quickdial" home — all styling and interactions — on a
 * machine that can't run the Go service.
 *
 *   cd dashboard
 *   npm run dev -- --config vite.demo.config.ts
 *   open http://localhost:5173/
 *
 * Delete this file to remove the demo entirely; it is not referenced by the app.
 */
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'
import type { IncomingMessage, ServerResponse } from 'node:http'

// --- Fixtures --------------------------------------------------------------

/** Inline an SVG string as a data URI so tile logos need no extra request. */
const svg = (markup: string): string => 'data:image/svg+xml,' + encodeURIComponent(markup)

// Each app's real brand mark (the canonical Simple Icons logo) as a TRANSPARENT
// brand-colour glyph — no plate/background. AppTile contains it (object-contain,
// padded) directly on its soft accent tint, exactly as a real uploaded logo
// would render. Fills are chosen to read on the light tint (the launcher
// default). These identify the apps; they are not otherwise modified.
const LOGOS = {
  grafana: svg(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='#F46800'><path d="M23.02 10.59a8.578 8.578 0 0 0-.862-3.034 8.911 8.911 0 0 0-1.789-2.445c.337-1.342-.413-2.505-.413-2.505-1.292-.08-2.113.4-2.416.62-.052-.02-.102-.044-.154-.064-.22-.089-.446-.172-.677-.247-.231-.073-.47-.14-.711-.197a9.867 9.867 0 0 0-.875-.161C14.557.753 12.94 0 12.94 0c-1.804 1.145-2.147 2.744-2.147 2.744l-.018.093c-.098.029-.2.057-.298.088-.138.042-.275.094-.413.143-.138.055-.275.107-.41.166a8.869 8.869 0 0 0-1.557.87l-.063-.029c-2.497-.955-4.716.195-4.716.195-.203 2.658.996 4.33 1.235 4.636a11.608 11.608 0 0 0-.607 2.635C1.636 12.677.953 15.014.953 15.014c1.926 2.214 4.171 2.351 4.171 2.351.003-.002.006-.002.006-.005.285.509.615.994.986 1.446.156.19.32.371.488.548-.704 2.009.099 3.68.099 3.68 2.144.08 3.553-.937 3.849-1.173a9.784 9.784 0 0 0 3.164.501h.08l.055-.003.107-.002.103-.005.003.002c1.01 1.44 2.788 1.646 2.788 1.646 1.264-1.332 1.337-2.653 1.337-2.94v-.058c0-.02-.003-.039-.003-.06.265-.187.52-.387.758-.6a7.875 7.875 0 0 0 1.415-1.7c1.43.083 2.437-.885 2.437-.885-.236-1.49-1.085-2.216-1.264-2.354l-.018-.013-.016-.013a.217.217 0 0 1-.031-.02c.008-.092.016-.18.02-.27.011-.162.016-.323.016-.48v-.253l-.005-.098-.008-.135a1.891 1.891 0 0 0-.01-.13c-.003-.042-.008-.083-.013-.125l-.016-.124-.018-.122a6.215 6.215 0 0 0-2.032-3.73 6.015 6.015 0 0 0-3.222-1.46 6.292 6.292 0 0 0-.85-.048l-.107.002h-.063l-.044.003-.104.008a4.777 4.777 0 0 0-3.335 1.695c-.332.4-.592.84-.768 1.297a4.594 4.594 0 0 0-.312 1.817l.003.091c.005.055.007.11.013.164a3.615 3.615 0 0 0 .698 1.82 3.53 3.53 0 0 0 1.827 1.282c.33.098.66.14.971.137.039 0 .078 0 .114-.002l.063-.003c.02 0 .041-.003.062-.003.034-.002.065-.007.099-.01.007 0 .018-.003.028-.003l.031-.005.06-.008a1.18 1.18 0 0 0 .112-.02c.036-.008.072-.013.109-.024a2.634 2.634 0 0 0 .914-.415c.028-.02.056-.041.085-.065a.248.248 0 0 0 .039-.35.244.244 0 0 0-.309-.06l-.078.042c-.09.044-.184.083-.283.116a2.476 2.476 0 0 1-.475.096c-.028.003-.054.006-.083.006l-.083.002c-.026 0-.054 0-.08-.002l-.102-.006h-.012l-.024.006c-.016-.003-.031-.003-.044-.006-.031-.002-.06-.007-.091-.01a2.59 2.59 0 0 1-.724-.213 2.557 2.557 0 0 1-.667-.438 2.52 2.52 0 0 1-.805-1.475 2.306 2.306 0 0 1-.029-.444l.006-.122v-.023l.002-.031c.003-.021.003-.04.005-.06a3.163 3.163 0 0 1 1.352-2.29 3.12 3.12 0 0 1 .937-.43 2.946 2.946 0 0 1 .776-.101h.06l.07.002.045.003h.026l.07.005a4.041 4.041 0 0 1 1.635.49 3.94 3.94 0 0 1 1.602 1.662 3.77 3.77 0 0 1 .397 1.414l.005.076.003.075c.002.026.002.05.002.075 0 .024.003.052 0 .07v.065l-.002.073-.008.174a6.195 6.195 0 0 1-.08.639 5.1 5.1 0 0 1-.267.927 5.31 5.31 0 0 1-.624 1.13 5.052 5.052 0 0 1-3.237 2.014 4.82 4.82 0 0 1-.649.066l-.039.003h-.287a6.607 6.607 0 0 1-1.716-.265 6.776 6.776 0 0 1-3.4-2.274 6.75 6.75 0 0 1-.746-1.15 6.616 6.616 0 0 1-.714-2.596l-.005-.083-.002-.02v-.056l-.003-.073v-.096l-.003-.104v-.07l.003-.163c.008-.22.026-.45.054-.678a8.707 8.707 0 0 1 .28-1.355c.128-.444.286-.872.473-1.277a7.04 7.04 0 0 1 1.456-2.1 5.925 5.925 0 0 1 .953-.763c.169-.111.343-.213.524-.306.089-.05.182-.091.273-.135.047-.02.093-.042.138-.062a7.177 7.177 0 0 1 .714-.267l.145-.045c.049-.015.098-.026.148-.041.098-.029.197-.052.296-.076.049-.013.1-.02.15-.033l.15-.032.151-.028.076-.013.075-.01.153-.024c.057-.01.114-.013.171-.023l.169-.021c.036-.003.073-.008.106-.01l.073-.008.036-.003.042-.002c.057-.003.114-.008.171-.01l.086-.006h.023l.037-.003.145-.007a7.999 7.999 0 0 1 1.708.125 7.917 7.917 0 0 1 2.048.68 8.253 8.253 0 0 1 1.672 1.09l.09.077.089.078c.06.052.114.107.171.159.057.052.112.106.166.16.052.055.107.107.159.164a8.671 8.671 0 0 1 1.41 1.978c.012.026.028.052.04.078l.04.078.075.156c.023.051.05.1.07.153l.065.15a8.848 8.848 0 0 1 .45 1.34.19.19 0 0 0 .201.142.186.186 0 0 0 .172-.184c.01-.246.002-.532-.024-.856z"/></svg>`),
  linear: svg(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='#5E6AD2'><path d="M2.886 4.18A11.982 11.982 0 0 1 11.99 0C18.624 0 24 5.376 24 12.009c0 3.64-1.62 6.903-4.18 9.105L2.887 4.18ZM1.817 5.626l16.556 16.556c-.524.33-1.075.62-1.65.866L.951 7.277c.247-.575.537-1.126.866-1.65ZM.322 9.163l14.515 14.515c-.71.172-1.443.282-2.195.322L0 11.358a12 12 0 0 1 .322-2.195Zm-.17 4.862 9.823 9.824a12.02 12.02 0 0 1-9.824-9.824Z"/></svg>`),
  github: svg(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='#181717'><path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12"/></svg>`),
  nextcloud: svg(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='#0082C9'><path d="M12.018 6.537c-2.5 0-4.6 1.712-5.241 4.015-.56-1.232-1.793-2.105-3.225-2.105A3.569 3.569 0 0 0 0 12a3.569 3.569 0 0 0 3.552 3.553c1.432 0 2.664-.874 3.224-2.106.641 2.304 2.742 4.016 5.242 4.016 2.487 0 4.576-1.693 5.231-3.977.569 1.21 1.783 2.067 3.198 2.067A3.568 3.568 0 0 0 24 12a3.569 3.569 0 0 0-3.553-3.553c-1.416 0-2.63.858-3.199 2.067-.654-2.284-2.743-3.978-5.23-3.977zm0 2.085c1.878 0 3.378 1.5 3.378 3.378 0 1.878-1.5 3.378-3.378 3.378A3.362 3.362 0 0 1 8.641 12c0-1.878 1.5-3.378 3.377-3.378zm-8.466 1.91c.822 0 1.467.645 1.467 1.468s-.644 1.467-1.467 1.468A1.452 1.452 0 0 1 2.085 12c0-.823.644-1.467 1.467-1.467zm16.895 0c.823 0 1.468.645 1.468 1.468s-.645 1.468-1.468 1.468A1.452 1.452 0 0 1 18.98 12c0-.823.644-1.467 1.467-1.467z"/></svg>`),
  salesforce: svg(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='#00A1E0'><path d="M10.006 5.415a4.195 4.195 0 013.045-1.306c1.56 0 2.954.9 3.69 2.205.63-.3 1.35-.45 2.1-.45 2.85 0 5.159 2.34 5.159 5.22s-2.31 5.22-5.176 5.22c-.345 0-.69-.044-1.02-.104a3.75 3.75 0 01-3.3 1.95c-.6 0-1.155-.15-1.65-.375A4.314 4.314 0 018.88 20.4a4.302 4.302 0 01-4.05-2.82c-.27.062-.54.076-.825.076-2.204 0-4.005-1.8-4.005-4.05 0-1.5.811-2.805 2.01-3.51-.255-.57-.39-1.2-.39-1.846 0-2.58 2.1-4.65 4.65-4.65 1.53 0 2.85.705 3.72 1.8"/></svg>`),
  vault: svg(`<svg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='#14161A'><path d="M0 0l11.955 24L24 0zm13.366 4.827h1.393v1.38h-1.393zm-2.77 5.569H9.22V8.993h1.389zm0-2.087H9.22V6.906h1.389zm0-2.086H9.22V4.819h1.389zm2.087 6.263h-1.377V11.08h1.388zm0-2.09h-1.377V8.993h1.388zm0-2.087h-1.377V6.906h1.388zm0-2.086h-1.377V4.819h1.388zm.683.683h1.393v1.389h-1.393zm0 3.475V8.993h1.389v1.388Z"/></svg>`),
}

/** Brand mark served at /branding/icon (favicon) — lucide ShieldCheck, ember. */
const FAVICON =
  `<svg xmlns='http://www.w3.org/2000/svg' width='32' height='32' viewBox='0 0 24 24' fill='none' stroke='#e0892f' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'><path d='M20 13c0 5-3.5 7.5-7.66 8.95a1 1 0 0 1-.67-.01C7.5 20.5 4 18 4 13V6a1 1 0 0 1 1-1c2 0 4.5-1.2 6.24-2.72a1.17 1.17 0 0 1 1.52 0C14.51 3.81 17 5 19 5a1 1 0 0 1 1 1z'/><path d='m9 12 2 2 4-4'/></svg>`

// GET /api/prohibitorum/me — admin so the tile "Manage app" item and the
// account menu's Admin entry are both visible. avatarPending:false → no poll.
const ME = {
  id: 1,
  username: 'jesse',
  displayName: 'Jesse Cheng',
  role: 'admin',
  avatarPending: false,
}

// GET /api/prohibitorum/config — no custom icon → the ShieldCheck brand mark.
const CONFIG = { instanceName: 'Prohibitorum', hasCustomIcon: false, iconUrl: '', iconEtag: '' }

// GET /api/prohibitorum/me/apps — deliberately diverse so every tile path shows:
//  - real brand logo on an accent-derived tint: Grafana/Linear/GitHub/Salesforce/Nextcloud/Vault
//  - generated duotone-gradient monogram (no logo): Internal Wiki
//  - all three protocol glyphs: oidc (key) / saml (fingerprint) / forward_auth (network)
const APPS = [
  { kind: 'forward_auth', id: 'grafana', name: 'Grafana', iconUrl: LOGOS.grafana, accentColor: '#f46800', launchUrl: 'https://grafana.example.com' },
  { kind: 'oidc', id: 'linear', name: 'Linear', iconUrl: LOGOS.linear, accentColor: '#5e6ad2', launchUrl: 'https://linear.example.com' },
  { kind: 'saml', id: 'github', name: 'GitHub Enterprise', iconUrl: LOGOS.github, accentColor: '#181717', launchUrl: 'https://github.example.com' },
  { kind: 'saml', id: 'salesforce', name: 'Salesforce', iconUrl: LOGOS.salesforce, accentColor: '#00a1e0', launchUrl: 'https://sso.example.com/saml/init' },
  { kind: 'oidc', id: 'nextcloud', name: 'Nextcloud', iconUrl: LOGOS.nextcloud, accentColor: '#0082c9', launchUrl: 'https://cloud.example.com' },
  { kind: 'oidc', id: 'vault', name: 'Vault', iconUrl: LOGOS.vault, accentColor: '#ffec6e', launchUrl: 'https://vault.example.com' },
  { kind: 'forward_auth', id: 'wiki', name: 'Internal Wiki', iconUrl: null, accentColor: null, launchUrl: 'https://wiki.example.com' },
]

// GET /api/prohibitorum/me/consent — the "connected" apps: OIDC consents +
// SAML acknowledgements, each tagged by `kind` (unified consent list). These are
// the apps shown on the home grid. Forward-auth apps (grafana, wiki) are always
// connected. The remaining authorized apps — salesforce (saml) and nextcloud
// (oidc) — are NOT here, so they appear under "+ Add app" as available to
// connect. Mutated by revoke below.
let CONSENTS = [
  { kind: 'oidc', clientId: 'linear', scopes: ['openid', 'profile', 'email'] },
  { kind: 'oidc', clientId: 'vault', scopes: ['openid', 'profile'] },
  { kind: 'saml', clientId: 'github', scopes: [] },
]

// --- Mock middleware -------------------------------------------------------

function send(res: ServerResponse, status: number, body: unknown, type = 'application/json'): void {
  res.statusCode = status
  res.setHeader('Content-Type', type)
  res.end(typeof body === 'string' ? body : JSON.stringify(body))
}

function readBody(req: IncomingMessage): Promise<string> {
  return new Promise((resolve) => {
    let data = ''
    req.on('data', (c) => { data += c })
    req.on('end', () => resolve(data))
    req.on('error', () => resolve(''))
  })
}

function mockApi(): Plugin {
  return {
    name: 'prohibitorum-demo-mock',
    // Installed in the configureServer BODY (not a returned hook) so it runs
    // BEFORE Vite's internal middlewares — our JSON wins over the SPA fallback.
    configureServer(server) {
      server.middlewares.use((req: IncomingMessage, res: ServerResponse, next: () => void) => {
        void (async () => {
          const url = (req.url ?? '').split('?')[0]
          const method = req.method ?? 'GET'

          if (url === '/branding/icon') return send(res, 200, FAVICON, 'image/svg+xml')
          if (!url.startsWith('/api/prohibitorum/')) return next()

          const p = url.slice('/api/prohibitorum'.length) // '/me', '/me/apps', ...

          if (method === 'GET' && p === '/me') return send(res, 200, ME)
          if (method === 'GET' && p === '/config') return send(res, 200, CONFIG)
          if (method === 'GET' && p === '/me/apps') return send(res, 200, APPS)
          if (method === 'GET' && p === '/me/consent') return send(res, 200, CONSENTS)
          if (method === 'GET' && p === '/me/avatar/status') return send(res, 200, { pending: false })

          if (method === 'POST' && p === '/me/consent/revoke') {
            let id = ''
            try { id = (JSON.parse(await readBody(req)) as { clientId?: string }).clientId ?? '' } catch { /* empty */ }
            CONSENTS = CONSENTS.filter((c) => c.clientId !== id)
            return send(res, 200, {})
          }

          // Any other API call (e.g. navigating into settings/admin, which this
          // demo intentionally does not stub) returns a clean JSON 404.
          return send(res, 404, { code: 'not_found', message: `demo mock: no stub for ${method} ${p}` })
        })()
      })
    },
  }
}

// --- Config ----------------------------------------------------------------

export default defineConfig({
  plugins: [vue(), tailwindcss(), mockApi()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  // No server.proxy: every backend call is answered by the mock above.
})
