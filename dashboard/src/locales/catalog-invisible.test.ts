import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * No catalogue may hide zero-width characters in its text: they cannot be seen
 * in review, and they break searching for a phrase. Line breaks are settled in
 * CSS or by rewording the sentence.
 */
const invisible = /\u2060|\u200B|\u200C|\u200D|\uFEFF/;

describe.each(["en", "zh"])("the %s catalogue", (locale) => {
  it("has no word joiner, zero-width space or byte-order mark", () => {
    const text = readFileSync(
      resolve(process.cwd(), `src/locales/${locale}/messages.po`),
      "utf8",
    );
    const lines = text
      .split("\n")
      .map((line, index) => ({ line, number: index + 1 }))
      .filter(({ line }) => invisible.test(line))
      .map(({ number }) => number);
    expect(lines).toEqual([]);
  });
});
