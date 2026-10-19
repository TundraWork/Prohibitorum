import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { TriangleAlert } from "lucide-react";
import { useId } from "react";
import { readPrincipalSource } from "@/api/federation";
import { principalSourceLabel } from "@/components/custom/principal-sources";
import { SrDescription } from "@/components/custom/SrDescription";
import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { i18n } from "@/i18n";

/**
 * The cells the four federation lists share.
 *
 * Identity providers, OIDC applications, SAML applications and forward-auth
 * applications are four views of the same thing — a record an operator opens to
 * change — and their tables had grown four separate answers to the same
 * questions. The questions are: what is this, how is it configured, and is
 * anything wrong with it. The first is `EntityCell`; the rest are here.
 *
 * Keeping them in one module is what stops the four lists drifting apart again:
 * a rule that lives on one page will be re-decided on the next one, differently.
 */

/**
 * The width of each of the two data columns on the application lists (OIDC,
 * SAML, forward-auth), so the three lists put their columns on the same lines
 * and the columns stay put as rows load, filter or run out. The application
 * column in front of them is the flexible one.
 */
export const APP_DATA_COLUMN = "w-56 max-w-56";

/**
 * The one state that gets to speak: a provider whose configuration is not
 * finished.
 *
 * There is deliberately no chip for a disabled row. A record that is switched
 * off is usually a settled decision rather than a fault, and a column of
 * "Disabled / Disabled / —" is mostly the word "Disabled": it costs the measure,
 * and it buries the row that genuinely needs attention. Disabled is carried
 * instead by the row's own recession — see `EntityCell`'s `dimmed` — which is
 * quieter and still reads at a glance down the column.
 *
 * `not_ready` is the opposite kind of fact: it is unfinished configuration, it
 * blocks the operator from enabling the provider at all, and it is the reason
 * they opened this page. It is the only state worth a column, and the only one
 * that raises its voice.
 *
 * The dot marks a status apart from a label; the words carry the meaning and the colour is the second signal,
 * as `PRODUCT.md` requires.
 */
export function NotReadyChip() {
  return (
    <Badge variant="warning">
      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
      <Trans id="list.state.not_ready">Not ready</Trans>
    </Badge>
  );
}

/**
 * A monospace value, with the whole of it in a tooltip because the table never
 * wraps and clips what will not fit.
 *
 * The tooltip is on every row rather than only the long values, because whether
 * a value fits depends on the viewport, and a rule that guessed would be wrong
 * at one width or the other.
 *
 * A monospace value is an identifier and reads in the muted colour at one size
 * on every list; a word ("Confidential", "Username") is sans in the
 * foreground colour. The two never swap, so a column of one kind scans as one.
 */
export function CodeValue({ value }: { value: string }) {
  return (
    <Tooltip>
      <TooltipTrigger delay={0} render={<span className="block truncate" />}>
        <span className="font-mono text-muted-foreground text-xs">{value}</span>
      </TooltipTrigger>
      <TooltipPopup className="break-all font-mono">{value}</TooltipPopup>
    </Tooltip>
  );
}

/**
 * The redirect addresses an OIDC application registers, as one cell.
 *
 * The count is what an operator checks — a client with none cannot sign anyone
 * in, and a client with more than one is a deliberate configuration — and the
 * first host is what tells them *which* application this is. The later hosts are
 * in the tooltip, since a row of six hosts would say less about the row than the
 * count does.
 *
 * A client with no redirect URI says so in words rather than drawing an empty
 * cell, in the warning colour with a triangle. That is the single most likely
 * misconfiguration on this list, so it must not look like a value that failed
 * to arrive, nor like an ordinary muted cell.
 */
export function RedirectCell({ uris }: { uris: readonly string[] }) {
  if (uris.length === 0) {
    return (
      <span
        data-tone="warning"
        className="inline-flex items-center gap-1.5 text-warning-foreground"
      >
        <TriangleAlert size={14} aria-hidden="true" />
        <Trans id="list.diagnostics.redirect.none">None</Trans>
      </span>
    );
  }

  return <RedirectHosts hosts={uris.map(hostOf)} />;
}

function RedirectHosts({ hosts }: { hosts: readonly string[] }) {
  const describedBy = useId();
  const count = hosts.length;
  // Joined in the console's language rather than the browser's.
  const hostList = new Intl.ListFormat(i18n.locale, {
    type: "conjunction",
  }).format(hosts);
  return (
    <>
      <Tooltip>
        <TooltipTrigger
          delay={0}
          aria-describedby={describedBy}
          render={<span />}
        >
          <span className="flex items-baseline gap-1.5">
            <span className="font-mono text-xs">{hosts[0]}</span>
            {hosts.length > 1 && (
              <span className="text-xs text-muted-foreground tabular-nums">
                +{hosts.length - 1}
              </span>
            )}
          </span>
        </TooltipTrigger>
        <TooltipPopup className="break-all font-mono">
          <span className="block text-muted-foreground">
            <Plural value={hosts.length} one="# address" other="# addresses" />
          </span>
          {hosts.map((host, index) => (
            // The host list is positional and never reordered.
            // biome-ignore lint/suspicious/noArrayIndexKey: see above
            <span key={index} className="block">
              {host}
            </span>
          ))}
        </TooltipPopup>
      </Tooltip>
      <SrDescription id={describedBy}>
        <Trans id="list.diagnostics.redirect.description">
          <Plural value={count} one="# address" other="# addresses" />:{" "}
          {hostList}
        </Trans>
      </SrDescription>
    </>
  );
}

/** The host of a redirect URI; the whole value when it is not a parseable URL. */
function hostOf(uri: string): string {
  try {
    return new URL(uri).host;
  } catch {
    return uri;
  }
}

/**
 * How many accounts hold an identity from an upstream provider.
 *
 * An operator's first question about a provider is whether anyone actually uses
 * it, which is what makes it safe to change or remove. `undefined` is a real
 * case — a response that does not carry the count — and reads as `—` rather than
 * as `0`, which would be a claim the data does not support.
 */
export function LinkedAccountsCell({ count }: { count: number | undefined }) {
  if (count === undefined)
    return <span className="text-muted-foreground">—</span>;
  return <span className="tabular-nums">{count}</span>;
}

/**
 * Which identity a downstream service receives for the person signing in.
 *
 * A forward-auth application hands the upstream service one of the account's
 * identifiers as the remote user, configurable per application. The server
 * narrows the value loosely, so it is read into the vocabulary the console
 * renders (`api/federation`); a value outside it is reported as absent rather
 * than printed raw, since an enum name is not something to show a reader.
 */
export function PrincipalSourceCell({ value }: { value: string }) {
  const { t } = useLingui();
  const source = readPrincipalSource(value);
  if (source === undefined)
    return <span className="text-muted-foreground">—</span>;

  return <span>{t(principalSourceLabel(source))}</span>;
}
