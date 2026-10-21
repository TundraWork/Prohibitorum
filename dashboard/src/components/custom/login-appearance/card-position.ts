import { cva } from "class-variance-authority";

/**
 * Where the sign-in stage and the wallpaper credit sit for each card position.
 * The public pages apply it from `lg`; a narrower window centres the stage and
 * puts the credit under it.
 *
 * The toolbar and the stage share the public region: the window, capped at
 * `--public-region` and centred. On the left or the right, the stage is
 * centred in that half of the region, so it keeps clear of the edge as the
 * screen grows; at `lg` the half is barely wider than the stage, which then
 * sits about where the toolbar capsule's edge does. The credit is fixed to the
 * window, so it takes the bottom corner of the region's other half, 1.25rem
 * in, and stops 1.5rem short of the middle: 2.75rem in all.
 */
export const publicCardPlacement = cva("", {
  variants: {
    position: {
      left: "lg:grid lg:grid-cols-2 lg:justify-items-center lg:*:col-start-1",
      center: "",
      right: "lg:grid lg:grid-cols-2 lg:justify-items-center lg:*:col-start-2",
    },
  },
});

/** The `lg` credit container on the public pages, fixed to the window. */
export const publicCreditPlacement = cva("fixed bottom-5 hidden lg:flex", {
  variants: {
    position: {
      left: "right-(--public-inset) max-w-[calc(min(50vw,var(--public-region)/2)-2.75rem)]",
      center:
        "left-(--public-inset) max-w-[calc(min(50vw,var(--public-region)/2)-17.75rem)]",
      right:
        "left-(--public-inset) max-w-[calc(min(50vw,var(--public-region)/2)-2.75rem)]",
    },
  },
});
