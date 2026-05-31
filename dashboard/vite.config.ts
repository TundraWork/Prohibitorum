import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

export default defineConfig({
  plugins: [vue(), ui()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/oauth': 'http://localhost:8080',
      '/saml': 'http://localhost:8080',
      '/oidc': 'http://localhost:8080',
      '/.well-known': 'http://localhost:8080',
    },
  },
  build: { outDir: '../pkg/webui/dist', emptyOutDir: true },
})
