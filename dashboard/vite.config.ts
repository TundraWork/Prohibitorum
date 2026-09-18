import { fileURLToPath, URL } from "node:url";
import { lingui, linguiTransformerBabelPreset } from "@lingui/vite-plugin";
import babel from "@rolldown/plugin-babel";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [
    react(),
    lingui({ failOnMissing: true, failOnCompileError: true }),
    babel({ presets: [linguiTransformerBabelPreset()] }),
    tailwindcss(),
  ],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/oauth": "http://localhost:8080",
      "/saml": "http://localhost:8080",
      "/oidc": "http://localhost:8080",
      "/.well-known": "http://localhost:8080",
    },
  },
  build: {
    outDir: "../pkg/webui/dist",
    emptyOutDir: true,
    // Fonts must remain same-origin files under the production CSP.
    assetsInlineLimit: (filePath) =>
      /\.(woff2?|ttf|otf|eot)$/i.test(filePath) ? false : undefined,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    environmentOptions: { jsdom: { url: "http://localhost/" } },
  },
});
