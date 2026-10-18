import { I18nProvider } from "@lingui/react";
import { act, render, renderHook, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { ApiError } from "@/api/errors";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { i18n } from "@/i18n";

beforeEach(() => i18n.activate("en"));

const refused = new ApiError({
  kind: "http",
  status: 400,
  code: "bad_request",
});

describe("useInlineFailure", () => {
  it("keeps a failure until it is cleared", () => {
    const { result } = renderHook(() => useInlineFailure());

    act(() => result.current.report(refused));
    expect(result.current.error).toBe(refused);

    act(() => result.current.clear());
    expect(result.current.error).toBeNull();
  });

  it("does not keep a cancellation, which is a decision and not a failure", () => {
    const { result } = renderHook(() => useInlineFailure());

    const cancelled = new Error("cancelled");
    cancelled.name = "SudoCancelled";
    act(() => result.current.report(cancelled));

    expect(result.current.error).toBeNull();
  });
});

describe("InlineFailure", () => {
  it("draws nothing without a failure", () => {
    render(
      <I18nProvider i18n={i18n}>
        <InlineFailure error={null} />
      </I18nProvider>,
    );

    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("says the failure as an alert", () => {
    render(
      <I18nProvider i18n={i18n}>
        <InlineFailure error={refused} />
      </I18nProvider>,
    );

    expect(screen.getByRole("alert")).toBeInTheDocument();
  });
});
