import { defineConfig } from "@lingui/cli";
import { formatter } from "@lingui/format-po";

export default defineConfig({
  locales: ["en", "zh"],
  // Naming the source locale is what puts each message's own text into the `en`
  // catalog. Without it the extractor only ever reports the source as "missing"
  // and leaves `msgstr` empty, so the English catalog has to be written by hand
  // — which is how 508 entries came to hold their own id instead of their text.
  sourceLocale: "en",
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
