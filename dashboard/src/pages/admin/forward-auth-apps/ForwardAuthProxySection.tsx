import { Trans, useLingui } from "@lingui/react/macro";
import { CheckIcon, CopyIcon, RouteIcon } from "lucide-react";
import type { CSSProperties } from "react";
import type { components } from "@/api/generated/schema";
import { Button } from "@/components/custom/Button";
import { CopyStatus } from "@/components/custom/CopyableValue";
import { inlineLinkClass } from "@/components/custom/inline-link";
import { Section, useSectionPanel } from "@/components/custom/Section";
import { useCopyValue } from "@/components/custom/use-copy-value";
import { FramePanel } from "@/components/ui/frame";
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
 * The block is read-only, one panel and the code on it. From `sm` up it scrolls
 * in both directions, since a configuration line is long and the block is a
 * reference, not the page; below that it wraps. The copy control is the
 * section's action, named in words, because it acts on the whole block rather
 * than on a spot inside it; its name does not change when it has copied, and it
 * shares its feedback with every other copy button on the console.
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
  // The panel is the code surface. Below `sm` a long line wraps under its own
  // text (a hanging indent) so none runs off the edge unseen; from `sm` up it
  // keeps its length and the block scrolls.
  const lines = snippet.split("\n");

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
          Use only behind a trusted reverse proxy. For entrypoint settings, see{" "}
          <a
            className={inlineLinkClass}
            href={manualUrl}
            target="_blank"
            rel="noreferrer"
          >
            the forward-auth guide
          </a>
          .
        </Trans>
      }
      action={
        <Button
          variant="outline"
          aria-label={t({
            id: "admin.forward-auth-apps.traefik.copy-label",
            message: "Copy the Traefik configuration",
          })}
          onClick={() => copy(snippet)}
        >
          {state === "copied" ? (
            <CheckIcon aria-hidden="true" className="text-success" />
          ) : (
            <CopyIcon aria-hidden="true" />
          )}
          <Trans id="admin.forward-auth-apps.traefik.copy">Copy</Trans>
        </Button>
      }
    >
      <SnippetPanel lines={lines} />
      <CopyStatus state={state} />
    </Section>
  );
}

/**
 * The code on the section's own panel. Taking the panel here, inside the
 * section, is what stops the section drawing a second one around it.
 */
function SnippetPanel({ lines }: { lines: string[] }) {
  const { t } = useLingui();
  useSectionPanel();
  return (
    <FramePanel className="p-0">
      {/* Focusable so a keyboard reader can scroll a block that overflows,
          and named itself so it is announced when it takes focus. */}
      {/* biome-ignore lint/a11y/useSemanticElements: the name belongs on the focusable block itself, which has to stay a <pre> */}
      <pre
        // biome-ignore lint/a11y/noNoninteractiveTabindex: a scroll container must be reachable by keyboard
        tabIndex={0}
        role="region"
        aria-label={t({
          id: "admin.forward-auth-apps.traefik.block",
          message: "Generated Traefik configuration",
        })}
        className="sm:max-h-96 overflow-auto rounded-xl p-4 font-mono text-xs leading-relaxed outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        {lines.map((line, at) => {
          const indent = line.length - line.trimStart().length;
          return (
            <span
              // biome-ignore lint/suspicious/noArrayIndexKey: the lines are a fixed rendering of one string
              key={at}
              className="block whitespace-pre-wrap [overflow-wrap:anywhere] ps-(--hang) -indent-(--hang) sm:whitespace-pre sm:[overflow-wrap:normal] sm:ps-0 sm:indent-0"
              style={{ "--hang": `${indent + 2}ch` } as CSSProperties}
            >
              {at < lines.length - 1 ? `${line}\n` : line}
            </span>
          );
        })}
      </pre>
    </FramePanel>
  );
}
