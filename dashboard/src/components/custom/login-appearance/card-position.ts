import { cva } from "class-variance-authority";

/**
 * Where the sign-in stage and the wallpaper credit sit for each card position.
 * The public pages apply it from `lg`; a narrower window centres the stage and
 * puts the credit under it. The settings preview is laid out 1280px wide, so it
 * applies its own rules at every width.
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

/** The preview's `main`, in place of the page's `mx-auto`. */
export const previewCardPlacement = cva("", {
  variants: {
    position: {
      left: "mr-auto ml-2",
      center: "mx-auto",
      right: "mr-2 ml-auto",
    },
  },
});

/** The credit inside the preview's footer, the page's rule with 1280px for `100vw`. */
export const previewCreditPlacement = cva("flex min-w-0", {
  variants: {
    position: {
      left: "ml-auto max-w-[calc(1280px-32.5rem)]",
      center: "max-w-[calc(640px-17rem)]",
      right: "max-w-[calc(1280px-32.5rem)]",
    },
  },
});
