import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";

/**
 * Every translation has to carry the same placeholders as its English text.
 *
 * Lingui fills `{name}` from the values the call site passes and `<0/>` from its
 * children, so a Chinese entry that drops one renders without it — "读取于"
 * with no time after it — and one that invents a name renders the literal
 * braces. Neither fails a build, so the catalogs are compared here.
 */

type Catalog = Map<string, string>;

function readCatalog(locale: string): Catalog {
  // Vitest serves this module over a non-file URL, so `import.meta.url` cannot
  // locate the catalog; the suite runs from the dashboard directory.
  const path = resolve(process.cwd(), `src/locales/${locale}/messages.po`);
  const catalog: Catalog = new Map();
  let id: string | undefined;
  let field: "msgid" | "msgstr" | undefined;
  let text = "";

  const flush = () => {
    if (field === "msgstr" && id !== undefined && id !== "") {
      catalog.set(id, text);
    }
  };

  for (const line of readFileSync(path, "utf8").split("\n")) {
    const start = /^(msgid|msgstr) "(.*)"$/.exec(line);
    if (start) {
      if (start[1] === "msgid") {
        flush();
        id = undefined;
      } else if (field === "msgid") {
        id = text;
      }
      field = start[1] as "msgid" | "msgstr";
      text = unquote(start[2] ?? "");
      continue;
    }
    const continued = /^"(.*)"$/.exec(line);
    if (continued && field !== undefined) {
      text += unquote(continued[1] ?? "");
    }
  }
  flush();
  return catalog;
}

function unquote(value: string): string {
  return value.replace(/\\(.)/g, (_, escaped: string) =>
    escaped === "n" ? "\n" : escaped,
  );
}

/**
 * What a message needs from its call site, read the way ICU reads it.
 *
 * `required` is the numbered elements (`<0>`, `</0>`, `<0/>`) and every
 * argument drawn as a value (`{name}`, `{n, number}`): a translation has to
 * carry each of them or the value is lost. `inflecting` is an argument the
 * message uses only to choose a plural or select branch; a language that does
 * not inflect there, as Chinese often does not, may leave it out. A `#` outside
 * any plural branch is printed as a literal "#", so one the source does not
 * also have is reported as well.
 */
interface Placeholders {
  required: string[];
  inflecting: string[];
  bareHash: boolean;
}

function placeholders(message: string): Placeholders {
  const required = new Set<string>();
  const inflecting = new Set<string>();
  let bareHash = false;
  let at = 0;

  for (const match of message.matchAll(/<\/?\d+\/?>/g)) required.add(match[0]);

  const skipSpace = () => {
    while (at < message.length && /\s/.test(message[at] ?? "")) at += 1;
  };
  const readWord = () => {
    const from = at;
    while (at < message.length && !/[\s,{}]/.test(message[at] ?? "")) at += 1;
    return message.slice(from, at);
  };

  // Reads text up to the `}` that closes the branch it is in, or the end.
  const text = (inPlural: boolean) => {
    while (at < message.length) {
      const char = message[at];
      if (char === "}") return;
      if (char === "{") argument(inPlural);
      else {
        if (char === "#" && !inPlural) bareHash = true;
        at += 1;
      }
    }
  };

  const argument = (inPlural: boolean) => {
    at += 1;
    skipSpace();
    const name = readWord();
    skipSpace();
    if (message[at] === "}") {
      at += 1;
      required.add(`{${name}}`);
      return;
    }
    at += 1;
    skipSpace();
    const type = readWord();
    if (!["plural", "select", "selectordinal"].includes(type)) {
      required.add(`{${name}}`);
      let depth = 1;
      while (at < message.length && depth > 0) {
        if (message[at] === "{") depth += 1;
        if (message[at] === "}") depth -= 1;
        at += 1;
      }
      return;
    }
    inflecting.add(`{${name}}`);
    skipSpace();
    if (message[at] === ",") at += 1;
    for (;;) {
      skipSpace();
      if (at >= message.length) return;
      if (message[at] === "}") {
        at += 1;
        return;
      }
      readWord();
      skipSpace();
      if (message[at] !== "{") return;
      at += 1;
      text(type === "select" ? inPlural : true);
      at += 1;
    }
  };

  text(false);
  for (const name of required) inflecting.delete(name);
  return {
    required: [...required].sort(),
    inflecting: [...inflecting].sort(),
    bareHash,
  };
}

/** Why a translation does not fit its source, or nothing when it does. */
function mismatch(source: string, translation: string): string | undefined {
  const expected = placeholders(source);
  const actual = placeholders(translation);
  // A `#` outside a plural is a literal, which is fine where the source means
  // one too; where the source has a count, it is the count gone missing.
  if (actual.bareHash && !expected.bareHash) return "# outside a plural";
  if (expected.required.join(" ") !== actual.required.join(" ")) {
    return `needs [${expected.required.join(" ")}], has [${actual.required.join(" ")}]`;
  }
  const invented = actual.inflecting.filter(
    (name) => !expected.inflecting.includes(name),
  );
  if (invented.length > 0) return `unknown [${invented.join(" ")}]`;
  return undefined;
}

describe("catalog placeholders", () => {
  it("reads arguments, elements and plural branches", () => {
    expect(
      placeholders("{count, plural, one {# account} other {# accounts}}"),
    ).toEqual({ required: [], inflecting: ["{count}"], bareHash: false });
    expect(placeholders("Read <0/> by {name}")).toEqual({
      required: ["<0/>", "{name}"],
      inflecting: [],
      bareHash: false,
    });
    expect(placeholders("删除 # 个作用域？").bareHash).toBe(true);
  });

  it("accepts a translation that drops an inflection its language lacks", () => {
    expect(
      mismatch("Keep {n, plural, one {it} other {them}}.", "保留它们。"),
    ).toBeUndefined();
    expect(mismatch("Read <0/>", "读取于")).toBeDefined();
    expect(mismatch("{0} accounts", "{count} 个账号")).toBeDefined();
  });

  it("matches between English and Chinese for every message", () => {
    const english = readCatalog("en");
    const chinese = readCatalog("zh");
    expect(english.size).toBeGreaterThan(0);

    const mismatches: string[] = [];
    for (const [id, source] of english) {
      const translation = chinese.get(id);
      if (translation === undefined) continue;
      const problem = mismatch(source, translation);
      if (problem !== undefined) {
        mismatches.push(
          `${id}: ${problem}\n  en: ${source}\n  zh: ${translation}`,
        );
      }
    }
    expect(mismatches, mismatches.join("\n")).toEqual([]);
  });
});
