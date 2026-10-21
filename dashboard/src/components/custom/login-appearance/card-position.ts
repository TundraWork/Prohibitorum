import { cva } from "class-variance-authority";

/**
 * Where the sign-in stage and the wallpaper credit sit for each card position.
 * The public pages apply it from `lg`; a narrower window centres the stage and
 * puts the credit under it.
 *
 * The toolbar and the stage share the public region: the window, capped at
 * `--public-region` and centred. On the left or the right, the stage's outer
 * edge lines up with the toolbar capsule's, 1.25rem inside the region. The
 * credit is fixed to the window, so it takes the bottom corner of the same
 * region and stops 1.5rem short of the stage, whichever width the stage
 * takes: 1.25rem to the region's edge, 1.5rem of gap, the 30rem wide stage and
 * its own 1.25rem to the far edge add up to 34rem.
 */
export const publicCardPlacement = cva("", {
  variants: {
    position: { left: "lg:justify-start", center: "", right: "lg:justify-end" },
  },
});

/** The `lg` credit container on the public pages, fixed to the window. */
export const publicCreditPlacement = cva("fixed bottom-5 hidden lg:flex", {
  variants: {
    position: {
      left: "right-(--public-inset) max-w-[calc(min(100vw,var(--public-region))-34rem)]",
      center:
        "left-(--public-inset) max-w-[calc(min(50vw,var(--public-region)/2)-17.75rem)]",
      right:
        "left-(--public-inset) max-w-[calc(min(100vw,var(--public-region))-34rem)]",
    },
  },
});
