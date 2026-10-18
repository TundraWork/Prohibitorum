import { cva } from "class-variance-authority";
import type { ReactNode } from "react";
import { Label } from "@/components/ui/label";
import { Radio } from "@/components/ui/radio-group";

// A card in the described-radio style: the selected one takes the primary
// border and a soft fill, and the radio's dot shows the choice as well.
const tile = cva(
  "group relative flex w-full cursor-pointer flex-col items-center rounded-lg border px-2 text-sm text-foreground/80 outline-none transition-[background-color,border-color,box-shadow,color,scale] duration-150 ease-out hover:bg-accent/50 has-focus-visible:ring-[3px] has-focus-visible:ring-ring/24 has-data-checked:border-primary/48 has-data-checked:bg-accent/50 has-data-checked:font-medium has-data-checked:text-foreground has-data-disabled:cursor-not-allowed has-data-disabled:opacity-64",
  {
    variants: {
      layout: {
        // An icon over a one-line name, at a fixed height.
        compact: "h-16 justify-center gap-1",
        // A picture over a name of up to two lines. Every tile in the row
        // takes the tallest one's height, so the pictures line up.
        media: "flex-1 justify-start gap-2 pt-3 pb-2.5",
      },
    },
    defaultVariants: { layout: "compact" },
  },
);

const name = cva("min-w-0 max-w-full", {
  variants: {
    layout: {
      compact: "truncate",
      media: "line-clamp-2 text-center wrap-anywhere",
    },
  },
  defaultVariants: { layout: "compact" },
});

/**
 * One choice in a `RadioGroup` drawn as a card: the card is the label of its
 * radio, whose dot sits in the corner of a `media` tile and before the name of
 * a `compact` one. Selection is a primary border, a soft fill, a heavier name
 * and the checked dot, so it does not rest on colour alone.
 *
 * The sign-in page's background source uses the `compact` tile, an icon over
 * a name; the profile page's avatar gallery uses `media`, a picture over a
 * name that may wrap once. The group lays the tiles out and names them.
 */
export function ChoiceTile({
  value,
  layout,
  media,
  label,
  title,
}: {
  value: string;
  layout?: "compact" | "media";
  /** The icon or picture above the name. */
  media: ReactNode;
  label: ReactNode;
  /** The whole name, for a `media` label the tile cuts short. */
  title?: string;
}) {
  const text = (
    <span className={name({ layout })} title={title}>
      {label}
    </span>
  );
  return (
    <Label className={tile({ layout })}>
      {layout === "media" ? (
        <>
          <Radio value={value} className="absolute start-2 top-2" />
          {media}
          {text}
        </>
      ) : (
        <>
          {media}
          <span className="flex max-w-full min-w-0 items-center gap-1.5">
            <Radio value={value} />
            {text}
          </span>
        </>
      )}
    </Label>
  );
}
