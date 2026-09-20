import { describe, expect, it } from "vitest";
import { rejectAvatar } from "@/pages/profile/AvatarPanel";

const MIB = 1024 * 1024;

describe("avatar upload precheck", () => {
  it("accepts an image within the server's limit", () => {
    expect(rejectAvatar({ size: 2 * MIB, type: "image/png" })).toBeNull();
    // The boundary is inclusive: exactly 5 MiB is still the server's call.
    expect(rejectAvatar({ size: 5 * MIB, type: "image/webp" })).toBeNull();
  });

  it("rejects a file past the limit before sending it", () => {
    expect(rejectAvatar({ size: 5 * MIB + 1, type: "image/png" })).toBe(
      "too_large",
    );
  });

  it("rejects a file that is not an image", () => {
    expect(rejectAvatar({ size: 1024, type: "application/pdf" })).toBe(
      "not_image",
    );
    expect(rejectAvatar({ size: 1024, type: "" })).toBe("not_image");
  });

  it("reports size before type, so a huge non-image still says 'too large'", () => {
    expect(rejectAvatar({ size: 50 * MIB, type: "text/plain" })).toBe(
      "too_large",
    );
  });
});
