import { defineConfig } from "@lingui/cli";
import { formatter } from "@lingui/format-po";

export default defineConfig({
  locales: ["en", "zh"],
  fallbackLocales: false,
  catalogs: [
    {
      path: "<rootDir>/src/locales/{locale}/messages",
      include: ["<rootDir>/src"],
      exclude: ["**/*.test.*", "**/test/**"],
    },
  ],
  format: formatter({ lineNumbers: false }),
  compileNamespace: "es",
});
