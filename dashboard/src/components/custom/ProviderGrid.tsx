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

/** Cells in the grid when folded: five providers plus the "More" cell. */
const FOLDED_CELLS = 6;

/**
 * The upstream providers a reader can sign in or enroll with, drawn to scale
 * with how many there are: one wide button, a two-column grid, or a grid of
 * five with the rest behind a searchable "More providers" popover.
 *
 * A press leaves the page for the provider's address and stays pending, with
 * every other control disabled, until the page is gone or comes back from the
 * back-forward cache. A page with other ways out passes `disabled` and hears
 * `onLeavingChange` to hold its own controls.
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
  const findProvider = t({
    id: "federation.find_provider",
    message: "Find a provider",
  });

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
          fallbackClassName="bg-foreground/10"
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

  const folded = providers.length >= FOLDED_CELLS + 1;
  const shown = folded ? providers.slice(0, FOLDED_CELLS - 1) : providers;
  const rest = folded ? providers.slice(FOLDED_CELLS - 1) : [];
  const needle = query.trim().toLocaleLowerCase();
  const count = rest.length;
  const matches = rest.filter((provider) =>
    provider.displayName.toLocaleLowerCase().includes(needle),
  );

  return (
    <div
      data-slot="provider-grid"
      className="grid grid-cols-1 min-[420px]:grid-cols-2 gap-2"
    >
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
            <span className="truncate" title={name}>
              {name}
            </span>
          </Button>
        );
      })}
      {folded && (
        <Popover onOpenChange={(open) => !open && setQuery("")}>
          <PopoverTrigger
            render={
              <Button
                variant="outline"
                disabled={disabled || leaving !== undefined}
                aria-label={t({
                  id: "federation.more_providers_count",
                  message: `${count} more providers`,
                })}
                className="min-w-0 justify-start text-muted-foreground"
              />
            }
          >
            <span aria-hidden="true" className="flex">
              <EntityAvatar
                size="sm"
                fallback={`+${rest.length}`}
                fallbackClassName="bg-foreground/10 text-muted-foreground"
              />
            </span>
            <span className="truncate">
              <Trans id="federation.more_providers">More providers</Trans>
            </span>
          </PopoverTrigger>
          <PopoverPopup
            className="w-72 dark:border-white/15 dark:shadow-2xl dark:shadow-black/60 **:data-[slot=popover-viewport]:py-1.5 **:data-[slot=popover-viewport]:[--viewport-inline-padding:--spacing(1.5)]"
            side="top"
            align="end"
          >
            <InputGroup className="mb-1.5">
              <InputGroupAddon>
                <SearchIcon aria-hidden="true" />
              </InputGroupAddon>
              <InputGroupInput
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={findProvider}
                aria-label={findProvider}
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
                <span className="truncate" title={provider.displayName}>
                  {provider.displayName}
                </span>
              </Button>
            ))}
          </PopoverPopup>
        </Popover>
      )}
    </div>
  );
}
