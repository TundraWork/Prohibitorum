import { cva } from "class-variance-authority";

/**
 * Where the sign-in stage and the wallpaper credit sit for each card position.
 * The public pages apply it from `lg`; a narrower window centres the stage and
 * puts the credit under it.
 *
 * On the left or the right, the stage is centred in that half of the window,
 * so a wide screen keeps it clear of the edge rather than pressed against it;
 * at `lg` the half is barely wider than the stage, which then sits about where
 * the toolbar capsule's edge does. The credit takes the bottom corner of the
 * other half and stops 1.5rem short of the middle: 1.25rem to the window and
 * 1.5rem of gap add up to 2.75rem.
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
      left: "right-5 max-w-[calc(50vw-2.75rem)]",
      center: "left-5 max-w-[calc(50vw-17.75rem)]",
      right: "left-5 max-w-[calc(50vw-2.75rem)]",
    },
  },
});
