import { Trans } from "@lingui/react/macro";
import { useEffect, useState } from "react";
import type { components } from "@/api/generated/schema";
import { loadDocument } from "@/app/load-document";
import { Button } from "@/components/custom/Button";
import { EntityAvatar } from "@/components/custom/EntityAvatar";

type FederationProvider = components["schemas"]["FederationProvider"];

/**
 * A button per upstream provider, for the sign-in and enrollment pages. Each
 * leaves the page for the server's address for that provider, which sends the
 * browser on to the provider itself.
 *
 * The pressed button stays pending and the others disabled until the page has
 * gone. A reader who comes back from the provider with the browser's back
 * button gets the page from the back-forward cache as it was left, so the
 * buttons come back when the page is shown again from there. With no
 * providers nothing is drawn, and the caller leaves out its separator too.
 *
 * A page with other ways out passes `isDisabled` while one of those is
 * running, and hears through `onLeavingChange` when a provider takes the
 * page away and when the page comes back, to hold its own controls.
 */
export function ProviderButtons({
  providers,
  href,
  isDisabled = false,
  onLeavingChange,
}: {
  providers: readonly FederationProvider[];
  href: (provider: FederationProvider) => string;
  isDisabled?: boolean;
  onLeavingChange?: (leaving: boolean) => void;
}) {
  const [leaving, setLeaving] = useState<string>();

  useEffect(() => {
    const restore = (event: PageTransitionEvent) => {
      if (!event.persisted) return;
      setLeaving(undefined);
      onLeavingChange?.(false);
    };
    window.addEventListener("pageshow", restore);
    return () => window.removeEventListener("pageshow", restore);
  }, [onLeavingChange]);

  if (providers.length === 0) return null;
  return (
    <div className="flex flex-col gap-2">
      {providers.map((provider) => {
        const name = provider.displayName;
        return (
          <Button
            key={provider.slug}
            variant="secondary"
            fullWidth
            isPending={leaving === provider.slug}
            isDisabled={
              isDisabled || (leaving !== undefined && leaving !== provider.slug)
            }
            onPress={() => {
              if (leaving !== undefined || isDisabled) return;
              setLeaving(provider.slug);
              onLeavingChange?.(true);
              loadDocument(href(provider));
            }}
          >
            {({ isPending }) => (
              <>
                {/* The spinner takes the icon's place while the page leaves. */}
                {!isPending && (
                  <span aria-hidden="true" className="flex">
                    <EntityAvatar
                      className="size-5 text-xs [&_[data-slot=avatar-fallback]]:bg-surface"
                      iconUrl={provider.iconUrl}
                      fallback={name.slice(0, 1)}
                    />
                  </span>
                )}
                <Trans id="federation.continue_with">
                  Continue with {name}
                </Trans>
              </>
            )}
          </Button>
        );
      })}
    </div>
  );
}
