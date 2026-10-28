import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath, URL } from "node:url";
import { promisify } from "node:util";
import { brotliCompress, constants, gzip } from "node:zlib";
import { lingui, linguiTransformerBabelPreset } from "@lingui/vite-plugin";
import babel from "@rolldown/plugin-babel";
import tailwindcss from "@tailwindcss/vite";
import { devtools } from "@tanstack/devtools-vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import type { Plugin } from "vite";
import { defineConfig } from "vitest/config";

const brotli = promisify(brotliCompress);
const gzipped = promisify(gzip);

/**
 * Writes a Brotli (`.br`) and a gzip (`.gz`) copy beside every script,
 * stylesheet and JSON file the build emits under `assets/`, at the highest
 * level of each: they are made once here, so the server sends them as they
 * are (`pkg/webui`) instead of compressing on every request or not at all.
 * The shell, `index.html`, is filled in per request and is left alone.
 */
function precompress(): Plugin {
  return {
    name: "prohibitorum:precompress",
    apply: "build",
    async writeBundle({ dir }, bundle) {
      if (dir === undefined) return;
      const files = Object.keys(bundle).filter(
        (file) => file.startsWith("assets/") && /\.(js|css|json)$/.test(file),
      );
      await Promise.all(
        files.map(async (file) => {
          const path = join(dir, file);
          const raw = await readFile(path);
          const [br, gz] = await Promise.all([
            brotli(raw, {
              params: {
                [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY,
                [constants.BROTLI_PARAM_SIZE_HINT]: raw.length,
              },
            }),
            gzipped(raw, { level: constants.Z_BEST_COMPRESSION }),
          ]);
          await Promise.all([
            writeFile(`${path}.br`, br),
            writeFile(`${path}.gz`, gz),
          ]);
        }),
      );
    },
  };
}

export default defineConfig({
  plugins: [
    // Must run before the framework plugins: it injects the source attributes
    // the inspector reads, pipes console output between the browser and the
    // terminal, and strips every Devtools import and element from the
    // production build. Its dev-only stages key off the Vite mode, so Vitest is
    // left untouched.
    devtools(),
    // Tests import the committed routeTree.gen.ts; generation would
    // double-evaluate route modules under Vitest's resetModules.
    ...(process.env.VITEST ? [] : [tanstackRouter({ target: "react" })]),
    react(),
    lingui({ failOnMissing: true, failOnCompileError: true }),
    babel({ presets: [linguiTransformerBabelPreset()] }),
    tailwindcss(),
    precompress(),
  ],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/branding": "http://localhost:8080",
      "/oauth": "http://localhost:8080",
      // The SAML endpoints all sit under `/saml/`; the bare prefix would also
      // take the dashboard's own `/saml-consent`.
      "^/saml/": "http://localhost:8080",
      "/oidc": "http://localhost:8080",
      "/.well-known": "http://localhost:8080",
      "/icon": "http://localhost:8080",
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
    // Each file gets its own jsdom, and the language-preference tests build a
    // whole router twice; on a loaded machine that alone runs to a few seconds
    // against Vitest's five-second default.
    testTimeout: 15000,
  },
});
