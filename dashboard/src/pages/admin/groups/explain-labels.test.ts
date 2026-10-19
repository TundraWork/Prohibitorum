import { beforeEach, describe, expect, it } from "vitest";
import { i18n } from "@/i18n";
import { explainSentence } from "@/pages/admin/groups/explain-labels";

beforeEach(() => i18n.activate("en"));

const sentence = (label: string, names: Record<string, string> = {}) =>
  explainSentence(
    label,
    (descriptor) => i18n._(descriptor),
    (slug) => names[slug],
  );

describe("the sentence for an explanation label", () => {
  it("names a provider, or hands back its slug when the catalogue lacks it", () => {
    expect(
      sentence("connection.provider=uni", { uni: "University SSO" }),
    ).toEqual({ provider: "University SSO" });
    expect(sentence("connection.provider=gone")).toEqual({
      unknownProvider: "gone",
    });
  });

  it("has no words for a protocol or a key it does not know", () => {
    expect(sentence("connection.protocol=ldap")).toBeUndefined();
    expect(sentence("geo.region=nl")).toBeUndefined();
    expect(sentence("something")).toBeUndefined();
  });
});
