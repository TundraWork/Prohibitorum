import { describe, expect, it } from "vitest";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import {
  changedSettings,
  colorError,
  intervalError,
  keyError,
  queryError,
  signInPageBody,
  signInPageMessages,
  suggestedColors,
} from "@/pages/admin/settings/sign-in-page/sign-in-page-form";

describe("the sign-in page rules the server shares", () => {
  it.each([
    ["#1f6f8b", undefined],
    ["#000000", undefined],
    ["#1F6F8B", signInPageMessages.colorInvalid],
    ["#fff", signInPageMessages.colorInvalid],
    ["1f6f8b", signInPageMessages.colorInvalid],
    ["#1f6f8g", signInPageMessages.colorInvalid],
  ])("colour %s", (value, expected) => {
    expect(colorError(value)).toBe(expected);
  });

  it.each([
    ["", undefined],
    ["snowy mountains", undefined],
    ["山".repeat(64), undefined],
    ["a".repeat(65), signInPageMessages.queryTooLong],
    [" sea", signInPageMessages.querySpaces],
    ["sea ", signInPageMessages.querySpaces],
    ["　sea", signInPageMessages.querySpaces],
    ["sea\tside", signInPageMessages.queryControl],
    ["sea\nside", signInPageMessages.queryControl],
  ])("keyword %j", (value, expected) => {
    expect(queryError(value)).toBe(expected);
  });

  it.each([
    [5, undefined],
    [3600, undefined],
    [15, undefined],
    [4, signInPageMessages.intervalRange],
    [3601, signInPageMessages.intervalRange],
    [7.5, signInPageMessages.intervalRange],
    [Number.NaN, signInPageMessages.intervalRange],
  ])("interval %d", (value, expected) => {
    expect(intervalError(value)).toBe(expected);
  });

  it.each([
    ["", "unsplash", false, signInPageMessages.keyRequired],
    ["", "unsplash", true, undefined],
    ["", "bing", false, undefined],
    ["Abc_123-xyz", "unsplash", false, undefined],
    ["a".repeat(128), "unsplash", false, undefined],
    ["a".repeat(129), "unsplash", false, signInPageMessages.keyInvalid],
    ["has space", "unsplash", true, signInPageMessages.keyInvalid],
    ["key.with.dots", "unsplash", true, signInPageMessages.keyInvalid],
  ] as const)(
    "key %j for %s (saved: %s)",
    (value, source, hasSavedKey, expected) => {
      expect(keyError(value, { source, hasSavedKey })).toBe(expected);
    },
  );
});

describe("the save body", () => {
  it("carries the whole appearance and leaves the key out when none was typed", () => {
    expect(
      signInPageBody({
        appearance: defaultLoginAppearance,
        unsplashAccessKey: "",
      }),
    ).toEqual({ appearance: defaultLoginAppearance });
  });

  it("carries a typed key as it was typed", () => {
    expect(
      signInPageBody({
        appearance: defaultLoginAppearance,
        unsplashAccessKey: "Abc_123",
      }),
    ).toEqual({
      appearance: defaultLoginAppearance,
      unsplashAccessKey: "Abc_123",
    });
  });

  it("carries the card's position and the theme as they were chosen", () => {
    const appearance = {
      ...defaultLoginAppearance,
      cardPosition: "right" as const,
      theme: "dark" as const,
    };
    const body = signInPageBody({ appearance, unsplashAccessKey: "" });
    expect(body.appearance.cardPosition).toBe("right");
    expect(body.appearance.theme).toBe("dark");
  });
});

describe("the suggested colours", () => {
  it("are eight different colours the colour field accepts", () => {
    expect(suggestedColors).toHaveLength(8);
    expect(new Set(suggestedColors).size).toBe(8);
    for (const hex of suggestedColors) expect(colorError(hex)).toBeUndefined();
  });
});

describe("counting the unsaved changes", () => {
  const edited = (edit: (a: typeof defaultLoginAppearance) => void) => {
    const draft = structuredClone(defaultLoginAppearance);
    edit(draft);
    return draft;
  };

  it("is zero for a draft that matches what is saved", () => {
    expect(
      changedSettings(defaultLoginAppearance, defaultLoginAppearance, ""),
    ).toBe(0);
  });

  it("counts each setting that differs", () => {
    const draft = edited((a) => {
      a.background.source = "gradient";
      a.background.gradient = "aurora";
      a.cardPosition = "left";
    });
    expect(changedSettings(defaultLoginAppearance, draft, "")).toBe(3);
  });

  it("counts a setting inside the card once", () => {
    const draft = edited((a) => {
      a.card.opacity = 35;
    });
    expect(changedSettings(defaultLoginAppearance, draft, "")).toBe(1);
  });

  it("counts a typed access key as a change", () => {
    expect(
      changedSettings(defaultLoginAppearance, defaultLoginAppearance, "Abc"),
    ).toBe(1);
  });

  it("does not count a setting put back as it was", () => {
    const draft = edited((a) => {
      a.theme = "dark";
      a.theme = "switchable";
    });
    expect(changedSettings(defaultLoginAppearance, draft, "")).toBe(0);
  });
});
