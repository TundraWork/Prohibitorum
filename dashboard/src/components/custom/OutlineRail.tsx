import { useLingui } from "@lingui/react/macro";
import { type ReactNode, useEffect, useState } from "react";
import {
  type RegisteredSection,
  useRegisteredSections,
} from "@/components/custom/SectionRegistry";
import { cn } from "@/lib/utils";

/** Where a section counts as "at the top": clear of the sticky console header. */
const TOP_OFFSET = 160;

function useCurrentSection(sections: RegisteredSection[]): string | undefined {
  const [current, setCurrent] = useState<string>();
  useEffect(() => {
    const update = () => {
      let found: string | undefined;
      for (const { id } of sections) {
        const top = document.getElementById(id)?.getBoundingClientRect().top;
        if (top !== undefined && top <= TOP_OFFSET) found = id;
      }
      setCurrent(found ?? sections[0]?.id);
    };
    update();
    // Capture: the page may scroll inside its own container, not the window.
    document.addEventListener("scroll", update, true);
    window.addEventListener("resize", update);
    return () => {
      document.removeEventListener("scroll", update, true);
      window.removeEventListener("resize", update);
    };
  }, [sections]);
  return current;
}

/**
 * The "On this page" list for an object page: one in-page link per registered
 * section, unsaved ones dotted, the danger section last, and `facts` under it.
 */
export function OutlineRail({
  facts,
}: {
  facts?: ReactNode;
}): React.ReactElement {
  const { t } = useLingui();
  const sections = useRegisteredSections();
  // Stable sort: the danger section moves to the end, the rest keep their order.
  const ordered = [
    ...sections.filter((section) => section.tone !== "danger"),
    ...sections.filter((section) => section.tone === "danger"),
  ];
  const current = useCurrentSection(ordered);

  return (
    <aside className="sticky top-[calc(var(--app-sticky-offset)+1.5rem)] grid gap-0.5 text-sm">
      <nav
        aria-label={t({ id: "outline.label", message: "On this page" })}
        className="grid gap-0.5"
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
            aria-current={current === section.id ? "location" : undefined}
            className={cn(
              "flex items-center rounded-md px-2 py-1.5 outline-none focus-visible:ring-2 focus-visible:ring-ring",
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
                className="ms-auto size-1.5 rounded-full bg-warning"
              />
            )}
          </a>
        ))}
      </nav>
      {facts !== undefined && (
        <>
          <div className="mx-2 my-4 border-t" />
          <div className="px-2 text-xs">{facts}</div>
        </>
      )}
    </aside>
  );
}
