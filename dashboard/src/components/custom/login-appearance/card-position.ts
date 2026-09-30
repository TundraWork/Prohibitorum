import { cva } from "class-variance-authority";

/**
 * Where the sign-in card and the wallpaper credit sit for each card position.
 * The public pages apply it from `lg`; a narrower window centres the card and
 * puts the credit under it. The settings preview is laid out 1280px wide, so it
 * applies it at every width.
 *
 * On the left or the right, the card's outer edge lines up with the toolbar
 * capsule's, 1.5rem from the window: `PublicCard`'s `main` is padded by 1rem,
 * so the margin on that side becomes 0.5rem. The credit takes the bottom corner
 * away from the card and stops 1.5rem short of it: 1.5rem to the window, 1.5rem
 * of gap, the 28rem card and its own 1.5rem to the far edge add up to 32.5rem.
 */
export const publicCardPlacement = cva("", {
  variants: {
    position: { left: "lg:ml-2", center: "", right: "lg:mr-2" },
  },
});

/** The `lg` credit container on the public pages, fixed to the window. */
export const publicCreditPlacement = cva("fixed bottom-5 hidden lg:flex", {
  variants: {
    position: {
      left: "right-6 max-w-[calc(100vw-32.5rem)]",
      center: "left-6 max-w-[calc(50vw-17rem)]",
      right: "left-6 max-w-[calc(100vw-32.5rem)]",
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
