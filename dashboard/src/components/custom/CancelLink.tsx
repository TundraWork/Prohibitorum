import { Trans } from "@lingui/react/macro";
import { Link, type LinkProps } from "@tanstack/react-router";
import { Button } from "@/components/custom/Button";

/**
 * Cancel for a create page: a link back to the list.
 *
 * While the create is `pending` it stays a link, so the element never changes
 * under the reader, but it is held: `aria-disabled`, out of the tab order,
 * dimmed, and its press goes nowhere, so the page cannot be left mid-create.
 */
export function CancelLink({
  to,
  pending = false,
}: {
  to: LinkProps["to"];
  pending?: boolean;
}) {
  return (
    <Button
      variant="ghost"
      aria-disabled={pending || undefined}
      tabIndex={pending ? -1 : undefined}
      className={pending ? "pointer-events-none opacity-64" : undefined}
      onClick={(event) => {
        if (pending) event.preventDefault();
      }}
      render={<Link to={to} />}
    >
      <Trans id="admin.cancel">Cancel</Trans>
    </Button>
  );
}
