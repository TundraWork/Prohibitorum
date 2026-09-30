import { parseColor } from "@heroui/react";
import { describe, expect, it } from "vitest";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";
import {
  colorError,
  intervalError,
  keyError,
  queryError,
  signInPageBody,
  signInPageMessages,
  suggestedColors,
} from "@/pages/admin/settings/sign-in-page-form";

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
});

describe("the suggested colours", () => {
  it.each(["en-US", "zh-CN"])(
    "each have their own name for screen readers in %s",
    (locale) => {
      const names = suggestedColors.map((hex) =>
        parseColor(hex).getColorName(locale),
      );
      expect(suggestedColors).toHaveLength(8);
      expect(new Set(names).size).toBe(names.length);
    },
  );
});
