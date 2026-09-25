import { describe, expect, it } from "vitest";
import {
  maxImageBytes,
  rejectImage,
} from "@/components/custom/ImageUploadControl";
import { avatarTypes } from "@/pages/profile/AvatarPanel";

const MIB = 1024 * 1024;
const avatar = { types: avatarTypes, maxBytes: maxImageBytes };
/** The sign-in background takes no GIF: `imageutil.ValidateRaw` refuses it. */
const background = {
  types: ["image/png", "image/jpeg", "image/webp"],
  maxBytes: maxImageBytes,
};

describe("image upload precheck", () => {
  it("accepts an image of an accepted type within the server's limit", () => {
    expect(
      rejectImage({ size: 2 * MIB, type: "image/png" }, avatar),
    ).toBeNull();
    // The boundary is inclusive: exactly 5 MiB is still the server's call.
    expect(
      rejectImage({ size: 5 * MIB, type: "image/webp" }, avatar),
    ).toBeNull();
  });

  it("rejects a file past the limit before sending it", () => {
    expect(rejectImage({ size: 5 * MIB + 1, type: "image/png" }, avatar)).toBe(
      "too_large",
    );
  });

  it("rejects a type the endpoint does not take, image or not", () => {
    expect(rejectImage({ size: 1024, type: "application/pdf" }, avatar)).toBe(
      "wrong_type",
    );
    expect(rejectImage({ size: 1024, type: "" }, avatar)).toBe("wrong_type");
    expect(rejectImage({ size: 1024, type: "image/bmp" }, avatar)).toBe(
      "wrong_type",
    );
    expect(rejectImage({ size: 1024, type: "image/gif" }, avatar)).toBeNull();
    expect(rejectImage({ size: 1024, type: "image/gif" }, background)).toBe(
      "wrong_type",
    );
  });

  it("reports size before type, so a huge non-image still says 'too large'", () => {
    expect(rejectImage({ size: 50 * MIB, type: "text/plain" }, avatar)).toBe(
      "too_large",
    );
  });
});
