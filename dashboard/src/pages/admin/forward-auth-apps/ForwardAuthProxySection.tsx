import { Trans, useLingui } from "@lingui/react/macro";
import { Check, ClipboardCopy } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { ScrollArea } from "@/components/custom/ScrollArea";
import { Section } from "@/components/custom/Section";
import { forwardAuthSnippet } from "@/pages/admin/forward-auth-apps/forward-auth-snippet";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * Where the manual lives. The page says the configuration belongs behind a
 * trusted proxy and links out for the rest: the requirements that make that
 * true — `forwardedHeaders.trustedIPs`, the proxy being the only path to the
 * backend, HTTPS — are several paragraphs of reasoning, and a console card is
 * the wrong place to read them.
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
 * saved `forwardAuthHost`, which is why this section re-reads the application
 * rather than taking a value from the general form: a host that has not been
 * saved is not the host the router will match.
 *
 * nginx is deliberately not offered: its `auth_request` cannot pass a `302`
 * back to the browser, and the verify endpoint answers an unauthenticated
 * request with exactly that. The manual says so; a second tab here would only
 * invite an operator to try it.
 */
export function ForwardAuthProxySection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  const [copied, setCopied] = useState(false);
  const snippet = forwardAuthSnippet({
    baseUrl: window.location.origin,
    host: app.forwardAuthHost,
  });

  return (
    <Section
      title={<Trans id="admin.forward-auth-apps.proxy">Reverse proxy</Trans>}
    >
      {/* The card keeps the console's measure and the snippet scrolls sideways
          inside it: a configuration line runs well past the column, and letting
          the card grow would break the rule that every block on a page reads the
          same width (`AGENTS.md`, "Layout and chrome"). Vertical overflow is
          capped for the same reason — the block is a reference, not the page. */}
      <ConsoleCard>
        {/* The copy control sits on the snippet's trailing edge, over the block
            rather than under it: it acts on what is beside it, and a button
            below a long block reads as belonging to the card instead. */}
        <div className="relative">
          <ScrollArea className="max-h-96 rounded-[0.375rem] bg-surface-secondary p-3 font-mono text-xs">
            <pre className="whitespace-pre">{snippet}</pre>
          </ScrollArea>
          <div className="absolute end-2 top-2">
            <Button
              isIconOnly
              size="sm"
              variant="ghost"
              aria-label={
                copied
                  ? t({
                      id: "admin.forward-auth-apps.proxy.copied",
                      message: "Copied",
                    })
                  : t({
                      id: "admin.forward-auth-apps.proxy.copy",
                      message: "Copy the configuration",
                    })
              }
              onPress={() => {
                void navigator.clipboard.writeText(snippet).then(
                  () => setCopied(true),
                  () => setCopied(false),
                );
              }}
            >
              {copied ? (
                <Check size={14} aria-hidden="true" />
              ) : (
                <ClipboardCopy size={14} aria-hidden="true" />
              )}
            </Button>
          </div>
        </div>

        <p className="mt-3 text-xs text-muted">
          <Trans id="admin.forward-auth-apps.proxy.note">
            Use this only behind a trusted reverse proxy, and set{" "}
            <span className="font-mono">forwardedHeaders.trustedIPs</span> on
            the entrypoint so Traefik replaces the client's own X-Forwarded-*
            headers. See{" "}
            <a
              className="underline"
              href={manualUrl}
              target="_blank"
              rel="noreferrer"
            >
              the forward-auth guide
            </a>{" "}
            for the full requirements.
          </Trans>
        </p>
      </ConsoleCard>
    </Section>
  );
}
