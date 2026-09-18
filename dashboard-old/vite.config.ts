import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/oauth': 'http://localhost:8080',
      '/saml': 'http://localhost:8080',
      '/oidc': 'http://localhost:8080',
      '/.well-known': 'http://localhost:8080',
    },
  },
  build: {
    outDir: '../pkg/webui/dist',
    emptyOutDir: true,
    // Never inline fonts as data: URIs — they must be served same-origin from
    // /assets so the strict `font-src 'self'` CSP (pkg/webui/webui.go) allows
    // them. (Vite's default 4 KB threshold inlines small woff2 subsets, which
    // the CSP then blocks.)
    assetsInlineLimit: (filePath: string) =>
      /\.(woff2?|ttf|otf|eot)$/i.test(filePath) ? false : undefined,
  },
})
