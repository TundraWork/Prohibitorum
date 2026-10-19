import { useLingui } from "@lingui/react/macro";
import { type ReactNode, useEffect, useRef, useState } from "react";
import {
  type RegisteredSection,
  useRegisteredSections,
} from "@/components/custom/SectionRegistry";
import { cn } from "@/lib/utils";

/** Where a section counts as "at the top": clear of the sticky console header. */
const TOP_OFFSET = 160;

/** How close to the end of the page counts as having reached it. */
const BOTTOM_SLACK = 4;

/** Input that means the reader, not a link jump, is moving the page. */
const READER_SCROLL_EVENTS = ["wheel", "touchmove", "keydown"] as const;

function atPageBottom(): boolean {
  const page = document.scrollingElement ?? document.documentElement;
  return (
    page.scrollHeight > page.clientHeight + BOTTOM_SLACK &&
    page.scrollTop + page.clientHeight >= page.scrollHeight - BOTTOM_SLACK
  );
}

/**
 * The section the reader is in: the last one whose top has passed the header.
 * A page that cannot scroll far enough to bring its last sections up to the
 * header counts the last section whose top is on screen once it is at the
 * bottom, and a rail entry the reader just chose counts at once and until
 * they scroll on their own.
 */
function useCurrentSection(sections: RegisteredSection[]): {
  current: string | undefined;
  choose: (id: string) => void;
} {
  const [current, setCurrent] = useState<string>();
  const chosen = useRef<string | undefined>(undefined);
  useEffect(() => {
    const update = () => {
      if (chosen.current !== undefined) {
        setCurrent(chosen.current);
        return;
      }
      const tops = sections.map(({ id }) => ({
        id,
        top: document.getElementById(id)?.getBoundingClientRect().top,
      }));
      let found: string | undefined;
      for (const { id, top } of tops) {
        if (top !== undefined && top <= TOP_OFFSET) found = id;
      }
      if (atPageBottom()) {
        for (const { id, top } of tops) {
          if (top !== undefined && top < window.innerHeight) found = id;
        }
      }
      setCurrent(found ?? sections[0]?.id);
    };
    const release = () => {
      if (chosen.current === undefined) return;
      chosen.current = undefined;
      update();
    };
    update();
    // Capture: the page may scroll inside its own container, not the window.
    document.addEventListener("scroll", update, true);
    window.addEventListener("resize", update);
    for (const name of READER_SCROLL_EVENTS) {
      window.addEventListener(name, release, { passive: true });
    }
    return () => {
      document.removeEventListener("scroll", update, true);
      window.removeEventListener("resize", update);
      for (const name of READER_SCROLL_EVENTS) {
        window.removeEventListener(name, release);
      }
    };
  }, [sections]);
  const choose = (id: string) => {
    chosen.current = id;
    setCurrent(id);
  };
  return { current, choose };
}

/**
 * The "On this page" list for an object page: one in-page link per registered
 * section, unsaved ones dotted, the danger section last, and `facts` under it.
 *
 * The rail is a single `minmax(0, 1fr)` track so a long unbreakable fact, such
 * as an issuer, truncates inside the column instead of widening it. Each entry
 * keeps a trailing slot for the unsaved dot, so a dot appearing never makes a
 * label wrap or the entries below it move.
 */
export function OutlineRail({
  facts,
  className,
}: {
  facts?: ReactNode;
  /** Layout classes for the rail, such as where it is hidden. */
  className?: string;
}): React.ReactElement {
  const { t } = useLingui();
  const sections = useRegisteredSections();
  // Stable sort: the danger section moves to the end, the rest keep their order.
  const ordered = [
    ...sections.filter((section) => section.tone !== "danger"),
    ...sections.filter((section) => section.tone === "danger"),
  ];
  const { current, choose } = useCurrentSection(ordered);

  return (
    <aside
      className={cn(
        "sticky top-[calc(var(--app-sticky-offset)+5rem)] grid min-w-0 grid-cols-[minmax(0,1fr)] gap-0.5 text-sm",
        className,
      )}
    >
      <nav
        aria-label={t({ id: "outline.label", message: "On this page" })}
        className="grid min-w-0 grid-cols-[minmax(0,1fr)] gap-0.5"
      >
        <div
          aria-hidden="true"
          className="px-2 pb-2 text-muted-foreground text-xs"
        >
          {t({ id: "outline.label", message: "On this page" })}
        </div>
        {ordered.map((section) => (
          <a
            key={section.id}
            href={`#${section.id}`}
            onClick={() => choose(section.id)}
            aria-current={current === section.id ? "location" : undefined}
            className={cn(
              "relative flex items-center rounded-md px-2 py-1.5 outline-none focus-visible:ring-2 focus-visible:ring-ring",
              section.tone === "danger"
                ? "text-destructive-foreground"
                : "text-muted-foreground hover:text-foreground",
              current === section.id && "bg-accent font-medium",
              current === section.id &&
                section.tone !== "danger" &&
                "text-foreground",
            )}
          >
            {section.title}
            {section.dirty && (
              <span
                role="img"
                aria-label={t({
                  id: "outline.unsaved",
                  message: "Unsaved changes",
                })}
                className="absolute end-0.5 top-1/2 size-1.5 -translate-y-1/2 rounded-full bg-warning"
              />
            )}
          </a>
        ))}
      </nav>
      {facts !== undefined && (
        <>
          <div className="mx-2 my-4 border-t" />
          <div className="min-w-0 px-2 text-xs">{facts}</div>
        </>
      )}
    </aside>
  );
}
