import { Trans } from "@lingui/react/macro";

/** The reference an administrator can look up a failure by, selectable whole. */
export function ReferenceLine({ reference: ref }: { reference: string }) {
  return (
    <p className="text-muted-foreground text-sm">
      <Trans id="error_page.reference">
        Reference{" "}
        <span className="select-all font-mono wrap-anywhere">{ref}</span>
      </Trans>
    </p>
  );
}
