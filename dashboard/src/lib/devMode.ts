// Dev-mode gate for the /dev console (a dev-only navigation/test hub, à la
// chrome://). True under the Vite dev server (import.meta.env.DEV) OR when the
// SPA is served from a loopback host — which is how the embedded dev-server
// (`mise dev-server`) runs at http://localhost:8080. A real deployment serves
// from its own hostname over HTTPS, so this is false there: the /dev route is
// guarded (redirects to /) and no dev link is shown. The view is also a lazy
// chunk, so its code is never fetched in a normal deployment.
//
// The console exposes nothing privileged — every action it offers (mint an
// invitation, open an API endpoint) already requires the same session/role the
// underlying API enforces. It is purely a manual-testing convenience.
export function isDevMode(): boolean {
  if (import.meta.env.DEV) return true
  const h = typeof window !== 'undefined' ? window.location.hostname : ''
  return h === 'localhost' || h === '127.0.0.1' || h === '::1' || h === '[::1]'
}
