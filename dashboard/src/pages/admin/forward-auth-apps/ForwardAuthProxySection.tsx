import { Link } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { Check, ClipboardCopy } from "lucide-react";
import { useEffect, useState } from "react";
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

/** How long the copy button says "Copied" before it offers to copy again. */
const copiedFor = 2000;

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
 * The card is `wide`: a configuration line is long, and in the reading measure
 * most of every line was behind the horizontal scroll. The copy control is the
 * section's action, named in words, because it acts on the whole block rather
 * than on a spot inside it.
 *
 * nginx is deliberately not offered: its `auth_request` cannot pass a `302`
 * back to the browser, and the verify endpoint answers an unauthenticated
 * request with exactly that. The manual says so; a second tab here would only
 * invite an operator to try it.
 */
export function ForwardAuthProxySection({ app }: { app: ForwardAuthApp }) {
  const [copied, setCopied] = useState(false);
  const snippet = forwardAuthSnippet({
    baseUrl: window.location.origin,
    host: app.forwardAuthHost,
  });

  useEffect(() => {
    if (!copied) return;
    const timer = window.setTimeout(() => setCopied(false), copiedFor);
    return () => window.clearTimeout(timer);
  }, [copied]);

  return (
    <Section
      title={
        <Trans id="admin.forward-auth-apps.traefik">
          Traefik configuration
        </Trans>
      }
      action={
        <Button
          size="sm"
          variant="secondary"
          onPress={() => {
            void navigator.clipboard.writeText(snippet).then(
              () => setCopied(true),
              () => setCopied(false),
            );
          }}
        >
          {copied ? (
            <>
              <Check size={16} aria-hidden="true" />
              <Trans id="admin.forward-auth-apps.traefik.copied">Copied</Trans>
            </>
          ) : (
            <>
              <ClipboardCopy size={16} aria-hidden="true" />
              <Trans id="admin.forward-auth-apps.traefik.copy">Copy</Trans>
            </>
          )}
        </Button>
      }
    >
      <ConsoleCard wide>
        {/* Vertical overflow is capped: the block is a reference, not the
            page. Lines longer than the card scroll sideways inside it. */}
        <ScrollArea className="max-h-96 rounded-[0.375rem] bg-surface-secondary p-3 font-mono text-xs">
          <pre className="whitespace-pre">{snippet}</pre>
        </ScrollArea>

        <p className="mt-3 text-xs text-muted">
          <Trans id="admin.forward-auth-apps.traefik.note">
            Use this only behind a trusted reverse proxy. See{" "}
            <Link
              className="text-xs"
              href={manualUrl}
              target="_blank"
              rel="noreferrer"
            >
              the forward-auth guide
            </Link>{" "}
            for the entrypoint settings.
          </Trans>
        </p>
      </ConsoleCard>
    </Section>
  );
}
