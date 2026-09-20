import type { ReactNode } from "react";

/**
 * The title of a console page. The console header carries the instance name, so
 * every page states its own name here to keep the heading hierarchy and the
 * document outline honest.
 */
export function PageHeading({ children }: { children: ReactNode }) {
  return <h1 className="text-2xl font-semibold wrap-anywhere">{children}</h1>;
}
