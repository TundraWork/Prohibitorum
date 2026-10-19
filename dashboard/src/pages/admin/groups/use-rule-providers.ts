import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import {
  groupProvidersQueryOptions,
  identityProvidersQueryOptions,
} from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import type {
  RuleProvider,
  RuleProvidersState,
} from "@/pages/admin/groups/RuleBuilder";

/**
 * The providers a rule can name, with the states the picker notes beside them.
 *
 * The catalogue lists every provider a rule may name, disabled ones included.
 * It carries no state, so a second read of the provider list supplies it. Both
 * only decorate the builder: a failed read is silent, and until the catalogue
 * arrives `known` is `undefined` and `state` says why, so no saved provider is
 * called unknown before anything has checked.
 */
export function useRuleProviders(): {
  providers: RuleProvider[];
  known: ReadonlySet<string> | undefined;
  state: RuleProvidersState;
} {
  const catalogue = useQuery(quietQuery(groupProvidersQueryOptions()));
  const states = useQuery(quietQuery(identityProvidersQueryOptions()));
  const entries = catalogue.data;
  const state: RuleProvidersState = catalogue.isError
    ? "failed"
    : entries === undefined
      ? "loading"
      : "loaded";
  const stateItems = states.data?.items;
  return useMemo(() => {
    if (entries === undefined)
      return { providers: [], known: undefined, state };
    const bySlug = new Map((stateItems ?? []).map((item) => [item.slug, item]));
    return {
      providers: entries.map((entry) => {
        const state = bySlug.get(entry.slug);
        return {
          slug: entry.slug,
          displayName: entry.displayName,
          disabled: state?.disabled === true,
          signInOff: state?.allowSignIn === false,
        };
      }),
      known: new Set(entries.map((entry) => entry.slug)),
      state,
    };
  }, [entries, stateItems, state]);
}
