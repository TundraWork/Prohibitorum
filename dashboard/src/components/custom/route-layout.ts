import { createContext } from "react";

/**
 * Which layout a page is drawn in. Each layout provides it around its outlet,
 * so a page's failure and its loading placeholder are drawn in that layout's
 * shape; outside both, they take the whole window.
 */
export const RouteLayoutContext = createContext<"console" | "public" | null>(
  null,
);
