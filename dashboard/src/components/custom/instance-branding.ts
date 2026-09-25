import { useSuspenseQuery } from "@tanstack/react-query";
import { publicConfigQueryOptions } from "@/api/queries";
import type { PublicConfig } from "@/api/raw-paths";

/**
 * What the instance calls itself and how it looks, as every surface draws it:
 * the console's sidebar and header, the sign-in toolbar, the sign-in background
 * and the document title.
 *
 * All of it comes from `GET /config`, which reports the effective values — an
 * override saved in the settings, else the deployment's configuration — so a
 * change saved on the settings page shows everywhere once that query refreshes.
 * The `instance-name` meta in `index.html` only titles the page before this
 * script runs; it is written at startup and never sees an override.
 */
export interface InstanceBranding {
  name: string;
  iconUrl: string;
  /** Set only when a custom sign-in background has been uploaded. */
  backgroundUrl?: string;
}

const fallbackName = "Prohibitorum";

/**
 * The server routes `/branding/*` by path and ignores the query string, which
 * makes the version parameter purely a cache key: the images are cached for a
 * few minutes, and a new ETag is a new URL the browser has not cached. A `data:`
 * URL carries its own content and would be corrupted by a suffix.
 */
export function versionedUrl(url: string, etag: string): string {
  if (etag === "" || url.startsWith("data:")) return url;
  const separator = url.includes("?") ? "&" : "?";
  return `${url}${separator}v=${encodeURIComponent(etag)}`;
}

export function instanceBranding(config: PublicConfig): InstanceBranding {
  return {
    name: config.instanceName || fallbackName,
    iconUrl: versionedUrl(config.iconUrl || "/branding/icon", config.iconEtag),
    ...(config.hasCustomBackground
      ? {
          backgroundUrl: versionedUrl(
            config.backgroundUrl || "/branding/background",
            config.backgroundEtag,
          ),
        }
      : {}),
  };
}

/**
 * The instance's branding. The root route loads `/config` before the first
 * paint, so this never suspends on a normal navigation.
 */
export function useInstanceBranding(): InstanceBranding {
  const { data } = useSuspenseQuery(publicConfigQueryOptions());
  return instanceBranding(data);
}
