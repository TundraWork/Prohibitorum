/**
 * isDevMode — true during local development.
 *
 * Vite sets import.meta.env.DEV=true during `npm run dev`; the loopback
 * check covers any scenario where the env flag isn't set but we're clearly
 * running locally (e.g. a production binary served from localhost for testing).
 */
export function isDevMode(): boolean {
  return (
    import.meta.env.DEV ||
    ['localhost', '127.0.0.1', '[::1]'].includes(window.location.hostname)
  )
}
