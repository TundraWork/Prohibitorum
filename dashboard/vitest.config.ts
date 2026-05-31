import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'
import ui from '@nuxt/ui/vite'

// The @nuxt/ui Vite plugin is registered here (not just @vitejs/plugin-vue) so
// that Nuxt UI's auto-imported components (<UButton>, <UInput>, <UFormField>,
// ...) resolve when components are mounted in component tests. Without it those
// tags fail to resolve / import. Mirrors vite.config.ts.
export default defineConfig({
  plugins: [vue(), ui()],
  test: { environment: 'jsdom' },
})
