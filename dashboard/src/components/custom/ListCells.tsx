import { Chip, Tooltip } from "@heroui/react";
import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { readPrincipalSource } from "@/api/federation";
import type { PrincipalSource } from "@/api/raw-admin-paths";

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
 * The dot follows HeroUI's own status-chip pattern, which marks a status apart
 * from a label; the words carry the meaning and the colour is the second signal,
 * as `PRODUCT.md` requires.
 */
export function NotReadyChip() {
  return (
    <Chip color="warning" size="sm" variant="soft">
      <span className="size-1.5 rounded-full bg-current" aria-hidden="true" />
      <Chip.Label>
        <Trans id="list.state.not_ready">Not ready</Trans>
      </Chip.Label>
    </Chip>
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
 * `muted` is for a value that is context rather than the thing being compared —
 * a remote-user source, a scope list. A value the reader copies out — a host, an
 * Entity ID — keeps the default foreground.
 */
export function CodeValue({
  value,
  muted = false,
}: {
  value: string;
  muted?: boolean;
}) {
  return (
    <Tooltip delay={0}>
      <Tooltip.Trigger<"span"> render={(props) => <span {...props} />}>
        <span className={`font-mono text-xs ${muted ? "text-muted" : ""}`}>
          {value}
        </span>
      </Tooltip.Trigger>
      <Tooltip.Content className="break-all font-mono">{value}</Tooltip.Content>
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
 * cell. That is the single most likely misconfiguration on this list, so it must
 * not look like a value that failed to arrive.
 */
export function RedirectCell({ uris }: { uris: readonly string[] }) {
  if (uris.length === 0) {
    return (
      <span className="text-muted">
        <Trans id="list.diagnostics.redirect.none">None</Trans>
      </span>
    );
  }

  const hosts = uris.map(hostOf);
  return (
    <Tooltip delay={0}>
      <Tooltip.Trigger<"span"> render={(props) => <span {...props} />}>
        <span className="flex items-baseline gap-1.5">
          <span className="font-mono text-xs">{hosts[0]}</span>
          {hosts.length > 1 && (
            <span className="text-xs text-muted tabular-nums">
              +{hosts.length - 1}
            </span>
          )}
        </span>
      </Tooltip.Trigger>
      <Tooltip.Content className="break-all font-mono">
        <span className="block text-muted">
          <Plural value={hosts.length} one="# address" other="# addresses" />
        </span>
        {hosts.map((host, index) => (
          // The host list is positional and never reordered.
          // biome-ignore lint/suspicious/noArrayIndexKey: see above
          <span key={index} className="block">
            {host}
          </span>
        ))}
      </Tooltip.Content>
    </Tooltip>
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
  if (count === undefined) return <span className="text-muted">—</span>;
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
  if (source === undefined) return <span className="text-muted">—</span>;

  const labels: Record<PrincipalSource, string> = {
    sub: t({ id: "list.principal-source.sub", message: "Subject" }),
    username: t({
      id: "list.principal-source.username",
      message: "Username",
    }),
    verified_email: t({
      id: "list.principal-source.verified-email",
      message: "Verified email",
    }),
  };
  return <span className="text-muted">{labels[source]}</span>;
}

/**
 * When a SAML application's signing certificate expires.
 *
 * This is the one fact on the SAML list that changes on its own and will
 * eventually break sign-in for the application, so it earns a column. It is a
 * date the reader plans around, which is why it is a date rather than a distance
 * from now — "expires 4 Mar 2027" is what an operator puts in a calendar, and
 * "in 5 months" is not.
 *
 * An expired certificate is the one state on this list that is already broken,
 * so it takes the danger colour as well as the date. A row with no signing key
 * says `—`, which is a different fact from a key that expired and has to stay
 * distinguishable from it.
 */
export function CertificateExpiryCell({ notAfter }: { notAfter?: string }) {
  const { i18n } = useLingui();

  if (notAfter === undefined) return <span className="text-muted">—</span>;
  const date = new Date(notAfter);
  if (Number.isNaN(date.getTime())) {
    return <span className="text-muted">—</span>;
  }

  const day = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
  }).format(date);
  const expired = date.getTime() < Date.now();

  return (
    <Tooltip delay={0}>
      <Tooltip.Trigger<"time">
        render={(props) => <time {...props} dateTime={notAfter} />}
      >
        <span className={expired ? "text-danger" : undefined}>{day}</span>
      </Tooltip.Trigger>
      <Tooltip.Content>
        {expired ? (
          <Trans id="list.diagnostics.certificate.expired">Expired</Trans>
        ) : (
          <Trans id="list.diagnostics.certificate.valid">
            Valid until this date
          </Trans>
        )}
      </Tooltip.Content>
    </Tooltip>
  );
}
