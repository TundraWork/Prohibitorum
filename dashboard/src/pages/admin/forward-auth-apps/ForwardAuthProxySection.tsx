import { Trans, useLingui } from "@lingui/react/macro";
import { CheckIcon, CopyIcon, RouteIcon } from "lucide-react";
import type { components } from "@/api/generated/schema";
import { Button } from "@/components/custom/Button";
import { CopyStatus } from "@/components/custom/CopyableValue";
import { inlineLinkClass } from "@/components/custom/inline-link";
import { Section } from "@/components/custom/Section";
import { useCopyValue } from "@/components/custom/use-copy-value";
import { forwardAuthSnippet } from "@/pages/admin/forward-auth-apps/forward-auth-snippet";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * Where the manual lives. The page says the configuration belongs behind a
 * trusted proxy and links out for the rest: the requirements that make that
 * true — `forwardedHeaders.trustedIPs`, the proxy being the only path to the
 * backend, HTTPS — are several paragraphs of reasoning, and a console section
 * is the wrong place to read them.
 */
const manualUrl =
  "https://github.com/TundraWork/Prohibitorum/blob/master/docs/forward-auth.md";

/**
 * The Traefik dynamic configuration this application needs.
 *
 * The snippet is generated rather than described because there is no server
 * endpoint for it: Traefik's configuration is the operator's, and the two
 * values that vary between applications are the host registered here and the
 * address this console answers on. Both are known to the page, so the page can
 * hand over something to paste rather than a list of fields to fill in.
 *
 * The address is `window.location.origin` — the origin the reader is looking at
 * the console from is the one their Traefik can reach it on. The host is the
 * saved `forwardAuthHost`, which is why this section reads the application
 * rather than the service form: a host that has not been saved is not the host
 * the router will match.
 *
 * The block is read-only and scrolls in both directions inside its well, since
 * a configuration line is long and the block is a reference, not the page. The
 * copy control is the section's action, named in words, because it acts on the
 * whole block rather than on a spot inside it; it shares its feedback with
 * every other copy button on the console.
 *
 * nginx is deliberately not offered: its `auth_request` cannot pass a `302`
 * back to the browser, and the verify endpoint answers an unauthenticated
 * request with exactly that. The manual says so; a second tab here would only
 * invite an operator to try it.
 */
export function ForwardAuthProxySection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  const { state, copy } = useCopyValue();
  const snippet = forwardAuthSnippet({
    baseUrl: window.location.origin,
    host: app.forwardAuthHost,
  });

  return (
    <Section
      id="traefik"
      icon={<RouteIcon />}
      title={t({
        id: "admin.forward-auth-apps.traefik",
        message: "Traefik configuration",
      })}
      description={
        <Trans id="admin.forward-auth-apps.traefik.note">
          Use this only behind a trusted reverse proxy. See{" "}
          <a
            className={inlineLinkClass}
            href={manualUrl}
            target="_blank"
            rel="noreferrer"
          >
            the forward-auth guide
          </a>{" "}
          for the entrypoint settings.
        </Trans>
      }
      action={
        <Button variant="outline" onClick={() => copy(snippet)}>
          {state === "copied" ? (
            <>
              <CheckIcon aria-hidden="true" className="text-success" />
              <Trans id="admin.forward-auth-apps.traefik.copied">Copied</Trans>
            </>
          ) : (
            <>
              <CopyIcon aria-hidden="true" />
              <Trans id="admin.forward-auth-apps.traefik.copy">Copy</Trans>
            </>
          )}
        </Button>
      }
    >
      <section
        aria-label={t({
          id: "admin.forward-auth-apps.traefik.block",
          message: "Generated Traefik configuration",
        })}
      >
        {/* Focusable so a keyboard reader can scroll a block that overflows. */}
        <pre
          // biome-ignore lint/a11y/noNoninteractiveTabindex: a scroll container must be reachable by keyboard
          tabIndex={0}
          className="max-h-96 overflow-auto rounded-xl bg-muted/60 p-4 font-mono text-xs leading-relaxed ring-1 ring-border/60 outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {snippet}
        </pre>
      </section>
      <CopyStatus state={state} />
    </Section>
  );
}
