import { Radio } from "@heroui/react";
import { cva } from "class-variance-authority";
import type { ReactNode } from "react";

// The selected outline is an inset ring and the focus ring an outer one, so a
// tile that is both — which is where a radio group's focus usually sits —
// shows both instead of one overwriting the other.
const tile = cva(
  "group flex w-full cursor-pointer flex-col items-center rounded-[0.375rem] bg-default/70 px-2 text-sm text-foreground/80 outline-none transition-[background-color,box-shadow,color,scale] duration-150 ease-out data-[hovered=true]:bg-default data-[focus-visible=true]:ring-2 data-[focus-visible=true]:ring-focus data-[selected=true]:bg-accent-soft data-[selected=true]:font-medium data-[selected=true]:text-accent-soft-foreground data-[selected=true]:inset-ring-1 data-[selected=true]:inset-ring-accent data-[pressed=true]:scale-[0.98]",
  {
    variants: {
      layout: {
        // An icon over a one-line name, at a fixed height.
        compact: "h-16 justify-center gap-1.5",
        // A picture over a name of up to two lines. Every tile in the row
        // takes the tallest one's height, so the pictures line up.
        media: "flex-1 justify-start gap-2 pt-3 pb-2.5",
      },
    },
    defaultVariants: { layout: "compact" },
  },
);

const name = cva("max-w-full", {
  variants: {
    layout: {
      compact: "truncate",
      media: "line-clamp-2 text-center wrap-anywhere",
    },
  },
  defaultVariants: { layout: "compact" },
});

/**
 * One choice in a `RadioGroup` drawn as a tile: the tile itself is the radio,
 * with no dot of its own. Selection is a soft accent fill with an inset
 * outline and a heavier name, so it does not rest on colour alone.
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
  return (
    <Radio value={value} className="m-0">
      <Radio.Content className={tile({ layout })}>
        {media}
        <span className={name({ layout })} title={title}>
          {label}
        </span>
      </Radio.Content>
    </Radio>
  );
}
