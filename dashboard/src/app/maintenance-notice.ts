import type { PublicConfig } from "@/api/raw-paths";
import type { Locale } from "@/i18n";
import { resolveLocale } from "@/i18n";

/** The notice a visitor sees, and the language it is written in. */
export interface MaintenanceNoticeText {
  text: string;
  locale: Locale;
}

/**
 * Picks the maintenance notice for the visitor's UI language, falling back to
 * the default language's notice. `null` when no language has one. The locale
 * tells the caller which language the text is in, so it can mark the element
 * when it differs from the UI language.
 */
export function pickMaintenanceNotice(
  config: Pick<
    PublicConfig,
    "maintenanceMessages" | "maintenanceDefaultLocale"
  >,
  locale: Locale,
): MaintenanceNoticeText | null {
  const own = config.maintenanceMessages[locale];
  if (own) return { text: own, locale };
  const fallback = resolveLocale(config.maintenanceDefaultLocale);
  const text =
    fallback === locale ? undefined : config.maintenanceMessages[fallback];
  if (text) return { text, locale: fallback };
  return null;
}
