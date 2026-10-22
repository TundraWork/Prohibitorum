import { cva } from "class-variance-authority";

/**
 * Where the sign-in stage and the wallpaper credit sit for each card position.
 * The public pages apply it from `lg`; a narrower window centres the stage and
 * puts the credit under it.
 *
 * Everything around the stage keeps `--public-edge` from the window: the
 * toolbar capsules, a stage set to the left or the right, and the credit in
 * the bottom corner away from the stage. The credit stops 1.5rem short of the
 * stage, whichever width the stage takes: its edge, 1.5rem of gap and the 30rem
 * wide stage, plus the stage's own edge.
 *
 * On a window at least 2200px wide, a side-placed stage moves onto a
 * panel of the page's own opaque background that takes two fifths of the
 * window on its side, centred in it, and the picture keeps the other three
 * fifths; the credit then stays within the picture.
 */
export const publicCardPlacement = cva("", {
  variants: {
    position: {
      left: "lg:justify-start min-[2200px]:grid min-[2200px]:grid-cols-[2fr_3fr] min-[2200px]:justify-items-center min-[2200px]:px-0 min-[2200px]:*:col-start-1",
      center: "",
      right:
        "lg:justify-end min-[2200px]:grid min-[2200px]:grid-cols-[3fr_2fr] min-[2200px]:justify-items-center min-[2200px]:px-0 min-[2200px]:*:col-start-2",
    },
  },
});

/** The opaque panel a side-placed stage sits on when the window is split. */
export const publicSplitPanel = cva(
  "pointer-events-none fixed inset-y-0 -z-[5] hidden w-2/5 bg-background min-[2200px]:block",
  {
    variants: {
      position: { left: "left-0", right: "right-0" },
    },
  },
);

/** The `lg` credit container on the public pages, fixed to the window. */
export const publicCreditPlacement = cva(
  "fixed bottom-(--public-edge) hidden lg:flex",
  {
    variants: {
      position: {
        left: "right-(--public-edge) max-w-[calc(100vw-2*var(--public-edge)-31.5rem)] min-[2200px]:max-w-[calc(60vw-2*var(--public-edge))]",
        center:
          "left-(--public-edge) max-w-[calc(50vw-var(--public-edge)-16.5rem)]",
        right:
          "left-(--public-edge) max-w-[calc(100vw-2*var(--public-edge)-31.5rem)] min-[2200px]:max-w-[calc(60vw-2*var(--public-edge))]",
      },
    },
  },
);
