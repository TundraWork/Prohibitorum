import { Trans, useLingui } from "@lingui/react/macro";
import { PaletteIcon } from "lucide-react";
import type { components } from "@/api/generated/schema";
import { EntityIconControl } from "@/components/custom/EntityIconCard";
import { Section } from "@/components/custom/Section";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * How the application is shown in the console and on the account's own
 * application list: its icon.
 *
 * The icon is its own write, made when the file is chosen, so the section has
 * no form and no save of its own. It sits apart from the service section
 * because it is drawn for every entity on the console from one component, and
 * the other kinds of application keep it in the same place.
 */
export function ForwardAuthAppearanceSection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  return (
    <Section
      id="appearance"
      icon={<PaletteIcon />}
      title={t({
        id: "admin.forward-auth-apps.appearance",
        message: "Appearance",
      })}
    >
      <div className="flex flex-col gap-2">
        <div className="font-medium text-sm">
          <Trans id="admin.forward-auth-apps.icon">Icon</Trans>
        </div>
        <EntityIconControl
          target={{ kind: "forward_auth", appId: app.clientId }}
          iconUrl={app.iconUrl}
          name={app.displayName || app.clientId}
        />
      </div>
    </Section>
  );
}
