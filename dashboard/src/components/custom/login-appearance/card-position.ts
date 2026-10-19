import { cva } from "class-variance-authority";

/**
 * Where the sign-in stage and the wallpaper credit sit for each card position.
 * The public pages apply it from `lg`; a narrower window centres the stage and
 * puts the credit under it.
 *
 * On the left or the right, the stage's outer edge lines up with the toolbar
 * capsule's, 1.25rem from the window. The credit takes the bottom corner away
 * from the stage and stops 1.5rem short of it, whichever width the stage takes:
 * 1.25rem to the window, 1.5rem of gap, the 30rem wide stage and its own 1.25rem
 * to the far edge add up to 34rem.
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
      left: "right-5 max-w-[calc(100vw-34rem)]",
      center: "left-5 max-w-[calc(50vw-17.75rem)]",
      right: "left-5 max-w-[calc(100vw-34rem)]",
    },
  },
});
