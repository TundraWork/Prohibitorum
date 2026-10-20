import { cva } from "class-variance-authority";
import { type ReactNode, useId } from "react";
import { Label } from "@/components/ui/label";
import { Radio } from "@/components/ui/radio-group";
import { cn } from "@/lib/utils";

// A card in the described-radio style: the selected one takes the primary
// border and a soft fill, and the radio's dot shows the choice as well.
const tile = cva(
  "group relative flex w-full cursor-pointer flex-col items-center rounded-lg border px-2 text-sm text-foreground/80 outline-none transition-[background-color,border-color,box-shadow,color,scale] duration-150 ease-out hover:bg-accent/50 has-focus-visible:ring-[3px] has-focus-visible:ring-ring/24 has-data-checked:border-primary/48 has-data-checked:bg-accent/50 has-data-checked:font-medium has-data-checked:text-foreground has-data-disabled:cursor-not-allowed has-data-disabled:border-dashed has-data-disabled:bg-muted/40 has-data-disabled:text-muted-foreground has-data-disabled:hover:bg-muted/40",
  {
    variants: {
      layout: {
        // An icon over a one-line name, at a fixed height.
        compact: "h-16 justify-center gap-1",
        // A picture over a name of up to two lines. Every tile in the row
        // takes the tallest one's height, so the pictures line up.
        media: "flex-1 justify-start gap-2 pt-8 pb-2.5",
        // The radio beside a name and the line saying what the choice means.
        list: "flex-row items-start gap-3 px-3 py-2.5",
      },
    },
    defaultVariants: { layout: "compact" },
  },
);

const name = cva("min-w-0 max-w-full", {
  variants: {
    layout: {
      compact: "truncate",
      media: "line-clamp-2 text-center wrap-break-word",
      list: "",
    },
  },
  defaultVariants: { layout: "compact" },
});

/**
 * One choice in a `RadioGroup` drawn as a card: the card is the label of its
 * radio. The dot sits before the name under the icon of a `compact` tile, and
 * in the top-start corner of a `media` tile, which reserves that corner so the
 * name keeps the full width. Selection is a primary border, a soft fill, a
 * heavier name and the checked dot, so it does not rest on colour alone.
 *
 * The sign-in page's background source uses the `compact` tile, an icon over
 * a name; the profile page's avatar gallery uses `media`, a picture over a
 * name that may wrap once. A `list` tile is one row of a stacked choice: the
 * dot, the name and a `description` under it, for options whose consequence
 * the reader has to weigh. The radio is named by the name alone and described
 * by the line under it. The group lays the tiles out and names them.
 */
export function ChoiceTile({
  value,
  layout,
  media,
  label,
  description,
  title,
  disabled,
  attached,
}: {
  value: string;
  layout?: "compact" | "media" | "list";
  /** The icon or picture above the name; a `list` tile has none. */
  media?: ReactNode;
  label: ReactNode;
  /** The line under a `list` tile's name. */
  description?: ReactNode;
  /** The whole name, for a `media` label the tile cuts short. */
  title?: string;
  disabled?: boolean;
  /**
   * Controls that belong to a chosen `list` tile, drawn as its continuation
   * under the name, so they read as part of the choice.
   */
  attached?: ReactNode;
}) {
  const nameId = useId();
  const descriptionId = useId();
  const text = (
    <span className={name({ layout })} title={title}>
      {label}
    </span>
  );
  const card = (
    <Label
      className={cn(
        tile({ layout }),
        attached !== undefined && "rounded-b-none",
      )}
    >
      {layout === "list" ? (
        <>
          <Radio
            value={value}
            disabled={disabled}
            aria-labelledby={nameId}
            aria-describedby={
              description === undefined ? undefined : descriptionId
            }
            className="mt-0.5"
          />
          <span className="flex min-w-0 flex-col gap-0.5">
            <span id={nameId} className="wrap-break-word">
              {label}
            </span>
            {description !== undefined && (
              <span
                id={descriptionId}
                className="font-normal text-muted-foreground text-sm"
              >
                {description}
              </span>
            )}
          </span>
        </>
      ) : layout === "media" ? (
        <>
          <Radio
            value={value}
            disabled={disabled}
            className="absolute start-2 top-2"
          />
          {media}
          {text}
        </>
      ) : (
        <>
          {media}
          <span className="flex max-w-full min-w-0 items-center gap-1.5">
            <Radio value={value} disabled={disabled} />
            {text}
          </span>
        </>
      )}
    </Label>
  );
  if (attached === undefined) return card;
  return (
    <div className="flex flex-col">
      {card}
      <div
        data-slot="choice-attached"
        className="rounded-b-lg border border-t-0 border-primary/48 bg-accent/50 px-3 pt-1 pb-3 ps-10"
      >
        {attached}
      </div>
    </div>
  );
}
