import {
  type MutationKey,
  useIsMutating,
  useMutation,
  useQueryClient,
} from "@tanstack/react-query";
import { useAtomValue, useSetAtom, useStore } from "jotai";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { updatePreferencesMutationOptions } from "@/api/mutations";
import { publicConfigQueryOptions, sessionQueryOptions } from "@/api/queries";
import {
  type ColorMode,
  forcedModeAtom,
  isColorMode,
  modeChoiceAtom,
  resolveAppearance,
  schemeChoiceAtom,
} from "@/lib/appearance";
import { type ColorSchemeSlug, isColorSchemeSlug } from "@/lib/color-scheme";

/**
 * Re-reads `read` from the query cache whenever the cache changes, without
 * asking the server. A disabled query observer would not do: signing in or out
 * removes the session query, and an observer stays on the removed one.
 */
function useCached<T>(read: (cache: ReturnType<typeof useQueryClient>) => T) {
  const queryClient = useQueryClient();
  const subscribe = useCallback(
    (onChange: () => void) => queryClient.getQueryCache().subscribe(onChange),
    [queryClient],
  );
  return useSyncExternalStore(subscribe, () => read(queryClient));
}

/** The session if one is already loaded. */
function useLoadedSession() {
  return useCached((queryClient) =>
    queryClient.getQueryData(sessionQueryOptions().queryKey),
  );
}

/**
 * The colour scheme and mode chosen, null where none was: the signed-in
 * account's saved ones first, then this browser's. Read from the account
 * directly, so a page never waits for the browser's copy to catch up.
 */
export function useChosenAppearance(): {
  scheme: ColorSchemeSlug | null;
  mode: ColorMode | null;
} {
  const saved = useLoadedSession()?.preferences.appearance;
  const scheme = useAtomValue(schemeChoiceAtom);
  const mode = useAtomValue(modeChoiceAtom);
  return {
    scheme: isColorSchemeSlug(saved?.scheme) ? saved.scheme : scheme,
    mode: isColorMode(saved?.mode) ? saved.mode : mode,
  };
}

/**
 * The colour scheme and mode the page shows. `known` is false while neither
 * this browser nor the loaded instance settings name a scheme, when the one
 * `/appearance-guard.js` applied stands.
 */
export function useResolvedAppearance() {
  const chosen = useChosenAppearance();
  const forced = useAtomValue(forcedModeAtom);
  const config = useCached((queryClient) =>
    queryClient.getQueryData(publicConfigQueryOptions().queryKey),
  );
  return {
    applied: resolveAppearance(chosen, config?.loginAppearance.scheme, forced),
    known: chosen.scheme !== null || config !== undefined,
  };
}

const preferencesKey = ["preferences"] as const;
const choiceKey = [...preferencesKey, "choice"] as const;

/**
 * A save of the account's preferences. Every one waits for the one before it,
 * so the server applies them in the order they were made and the last stands.
 * Each draws itself into the session at once; an answer that arrives while a
 * later save waits is not drawn, since it would put back what the reader has
 * already changed.
 */
function usePreferencesSave(mutationKey: MutationKey, reportsInline = false) {
  const queryClient = useQueryClient();
  const options = updatePreferencesMutationOptions(queryClient);
  return useMutation({
    ...options,
    mutationKey,
    scope: { id: "preferences" },
    ...(reportsInline ? { meta: { reportsInline: true } } : {}),
    onSuccess: (...answer) => {
      // This save still counts itself until its callbacks have run.
      if (queryClient.isMutating({ mutationKey: preferencesKey }) > 1) return;
      return options.onSuccess?.(...answer);
    },
  });
}

/**
 * The chosen colour scheme and mode, and a way to change them in this browser
 * that also saves the change to the signed-in account. The account gets the
 * whole appearance section, so a field never chosen is saved as unset.
 *
 * A choice shows at once and stays open to the next one while it saves; the
 * saves run in order, so the last choice is the one kept. `saving` names the
 * part last chosen here while any of them is still on its way, for the picker
 * to mark the chosen option with the wait.
 */
export function useAppearanceChoice() {
  const { scheme, mode } = useChosenAppearance();
  const setScheme = useSetAtom(schemeChoiceAtom);
  const setMode = useSetAtom(modeChoiceAtom);
  const session = useLoadedSession();
  const { mutate } = usePreferencesSave(choiceKey);
  const [picked, setPicked] = useState<"scheme" | "mode" | null>(null);
  const inFlight = useIsMutating({ mutationKey: choiceKey }) > 0;
  const choose = (next: { scheme?: ColorSchemeSlug; mode?: ColorMode }) => {
    const appearance = {
      scheme: next.scheme ?? scheme,
      mode: next.mode ?? mode,
    };
    if (next.scheme) setScheme(next.scheme);
    if (next.mode) setMode(next.mode);
    if (!session) return;
    setPicked(next.scheme ? "scheme" : "mode");
    mutate({ appearance });
  };
  return { scheme, mode, choose, saving: inFlight ? picked : null };
}

/**
 * Keeps this browser's choice and the signed-in account's in step. When the
 * session loads, an account that has saved an appearance refreshes the
 * browser's choice, which the sign-in page shows after signing out; an account
 * that has not takes the browser's, so a scheme picked on the sign-in page
 * stays after signing in and follows the account to other devices.
 */
export function useAccountAppearanceSync() {
  const store = useStore();
  const session = useLoadedSession();
  // Nobody asked for this save, so a failure is not reported; the choice
  // still applies in this browser.
  const { mutate } = usePreferencesSave([...preferencesKey, "sync"], true);
  // Offer the browser's choice once per account and page load, so a failed
  // save is not retried on every session refresh.
  const offered = useRef<number | null>(null);
  useEffect(() => {
    if (!session) return;
    const saved = session.preferences.appearance;
    if (saved.scheme !== null || saved.mode !== null) {
      if (isColorSchemeSlug(saved.scheme)) {
        store.set(schemeChoiceAtom, saved.scheme);
      }
      if (isColorMode(saved.mode)) store.set(modeChoiceAtom, saved.mode);
      return;
    }
    const scheme = store.get(schemeChoiceAtom);
    const mode = store.get(modeChoiceAtom);
    if ((scheme === null && mode === null) || offered.current === session.id) {
      return;
    }
    offered.current = session.id;
    mutate({ appearance: { scheme, mode } });
  }, [session, store, mutate]);
}
