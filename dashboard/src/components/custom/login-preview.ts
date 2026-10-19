import { createContext, useContext } from "react";
import { quietQuery } from "@/app/query-client";

/**
 * True while the sign-in page is drawn as a picture of itself, as in the
 * sign-in page editor's preview: it looks the same but never waits for a
 * passkey, reports on the browser or toasts a failed read, since the preview
 * says so in its own note.
 */
export const LoginPreviewContext = createContext(false);

/** A read of the sign-in page's data, quiet while that page is only a picture. */
export function usePreviewQuiet<O extends object>(options: O): O {
  return useContext(LoginPreviewContext) ? quietQuery(options) : options;
}
