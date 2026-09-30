import { expect, it } from "vitest";
import { planUploads } from "@/pages/admin/settings/LoginImagesControl";

function file(name: string, type: string, size = 10): File {
  const f = new File(["x"], name, { type });
  Object.defineProperty(f, "size", { value: size });
  return f;
}

it("takes files in the order chosen until the ten places are used", () => {
  const plan = planUploads(
    [
      file("a.png", "image/png"),
      file("b.webp", "image/webp"),
      file("c.jpg", "image/jpeg"),
    ],
    8,
  );
  expect(plan.accepted.map((f) => f.name)).toEqual(["a.png", "b.webp"]);
  expect(plan.skipped).toEqual({ too_large: 0, wrong_type: 0, full: 1 });
});

it("leaves out files that are too large or of the wrong kind before counting places", () => {
  const plan = planUploads(
    [
      file("huge.png", "image/png", 5 * 1024 * 1024 + 1),
      file("anim.gif", "image/gif"),
      file("ok.png", "image/png"),
    ],
    9,
  );
  expect(plan.accepted.map((f) => f.name)).toEqual(["ok.png"]);
  expect(plan.skipped).toEqual({ too_large: 1, wrong_type: 1, full: 0 });
});

it("accepts nothing once the list is full", () => {
  const plan = planUploads([file("a.png", "image/png")], 10);
  expect(plan.accepted).toEqual([]);
  expect(plan.skipped.full).toBe(1);
});
