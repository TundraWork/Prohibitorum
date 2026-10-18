import { Trans, useLingui } from "@lingui/react/macro";
import { SearchIcon } from "lucide-react";
import { useEffect, useState } from "react";
import type { components } from "@/api/generated/schema";
import { loadDocument } from "@/app/load-document";
import { Button } from "@/components/custom/Button";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
} from "@/components/ui/input-group";
import { Popover, PopoverPopup, PopoverTrigger } from "@/components/ui/popover";

type FederationProvider = components["schemas"]["FederationProvider"];

/** Providers drawn as buttons before the rest fold into the popover. */
const VISIBLE_LIMIT = 6;

/**
 * The upstream providers a reader can sign in or enroll with, drawn to scale
 * with how many there are: one wide button, a two-column grid, or a grid of
 * five with the rest behind a searchable "More providers" popover.
 *
 * A press leaves the page for the server's address for that provider, which
 * sends the browser on to the provider itself. The pressed control stays
 * pending and every other one disabled until the page has gone; a reader who
 * comes back with the browser's back button gets the page from the
 * back-forward cache as it was left, so the controls return when the page is
 * shown again from there. With no providers nothing is drawn, and the caller
 * leaves out its separator too.
 *
 * A page with other ways out passes `disabled` while one of those is running,
 * and hears through `onLeavingChange` when a provider takes the page away and
 * when it comes back, to hold its own controls.
 */
export function ProviderGrid({
  providers,
  href,
  disabled = false,
  onLeavingChange,
}: {
  providers: readonly FederationProvider[];
  href: (provider: FederationProvider) => string;
  disabled?: boolean;
  onLeavingChange?: (leaving: boolean) => void;
}) {
  const { t } = useLingui();
  const [leaving, setLeaving] = useState<string>();
  const [query, setQuery] = useState("");

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

  const leave = (provider: FederationProvider) => {
    if (leaving !== undefined || disabled) return;
    setLeaving(provider.slug);
    onLeavingChange?.(true);
    loadDocument(href(provider));
  };
  const stateOf = (provider: FederationProvider) => ({
    pending: leaving === provider.slug,
    disabled: disabled || (leaving !== undefined && leaving !== provider.slug),
  });
  // The spinner takes the tile's place while the page leaves.
  const tile = (provider: FederationProvider) =>
    leaving !== provider.slug && (
      <span aria-hidden="true" className="flex">
        <EntityAvatar
          size="sm"
          iconUrl={provider.iconUrl}
          name={provider.displayName}
        />
      </span>
    );

  if (providers.length === 1) {
    const [provider] = providers as [FederationProvider];
    const name = provider.displayName;
    return (
      <Button
        {...stateOf(provider)}
        onClick={() => leave(provider)}
        variant="outline"
        className="w-full"
      >
        {tile(provider)}
        <Trans id="federation.continue_with">Continue with {name}</Trans>
      </Button>
    );
  }

  const folded = providers.length >= VISIBLE_LIMIT + 1;
  const shown = folded ? providers.slice(0, VISIBLE_LIMIT - 1) : providers;
  const rest = folded ? providers.slice(VISIBLE_LIMIT - 1) : [];
  const needle = query.trim().toLocaleLowerCase();
  const matches = rest.filter((provider) =>
    provider.displayName.toLocaleLowerCase().includes(needle),
  );

  return (
    <div className="grid grid-cols-2 gap-2">
      {shown.map((provider) => {
        const name = provider.displayName;
        return (
          <Button
            key={provider.slug}
            {...stateOf(provider)}
            onClick={() => leave(provider)}
            variant="outline"
            className="min-w-0 justify-start"
            aria-label={t({
              id: "federation.continue_with",
              message: `Continue with ${name}`,
            })}
          >
            {tile(provider)}
            <span className="truncate">{name}</span>
          </Button>
        );
      })}
      {folded && (
        <Popover>
          <PopoverTrigger
            render={
              <Button
                variant="outline"
                disabled={disabled || leaving !== undefined}
                className="min-w-0 justify-start text-muted-foreground"
              />
            }
          >
            <span aria-hidden="true" className="flex">
              <EntityAvatar
                size="sm"
                fallback={`+${rest.length}`}
                className="bg-muted text-muted-foreground"
              />
            </span>
            <span className="truncate">
              <Trans id="federation.more_providers">More providers</Trans>
            </span>
          </PopoverTrigger>
          <PopoverPopup className="w-72 p-1.5" side="top" align="end">
            <InputGroup className="mb-1.5">
              <InputGroupAddon>
                <SearchIcon aria-hidden="true" />
              </InputGroupAddon>
              <InputGroupInput
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t({
                  id: "federation.find_provider",
                  message: "Find a provider",
                })}
                aria-label={t({
                  id: "federation.find_provider",
                  message: "Find a provider",
                })}
              />
            </InputGroup>
            {matches.length === 0 && (
              <p className="px-2 py-1.5 text-muted-foreground text-sm">
                <Trans id="federation.no_provider_match">
                  No provider matches
                </Trans>
              </p>
            )}
            {matches.map((provider) => (
              <Button
                key={provider.slug}
                {...stateOf(provider)}
                onClick={() => leave(provider)}
                variant="ghost"
                className="w-full justify-start"
              >
                {tile(provider)}
                <span className="truncate">{provider.displayName}</span>
              </Button>
            ))}
          </PopoverPopup>
        </Popover>
      )}
    </div>
  );
}
