import { Trans, useLingui } from "@lingui/react/macro";
import type { UseQueryResult } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describeError } from "@/api/errors";
import { Button } from "@/components/custom/Button";
import { InlineFailure } from "@/components/custom/InlineFailure";
import { ItemList } from "@/components/custom/ItemList";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * One list on an account's page: the section's heading from the first frame,
 * skeleton rows while the read is in flight, and the list, its empty state or
 * the read's failure in the same place afterwards.
 *
 * The four lists load independently and sit above the sections a reader may
 * have followed a link to, so each waits as the rows it is about to hold
 * rather than as a spinner. A failed read says so here with a way to ask
 * again; the section's own writes report through `writeError`, so a refused
 * removal is told once, above the list, and the list stays as it was.
 */
export function AccessSection({
  id,
  icon,
  title,
  description,
  label,
  query,
  empty,
  footer,
  writeError,
  children,
}: {
  id: string;
  icon: ReactNode;
  title: ReactNode;
  description: ReactNode;
  /** Names the list for assistive technology. */
  label: string;
  query: Pick<
    UseQueryResult<unknown>,
    "data" | "error" | "isPending" | "refetch"
  >;
  empty: ReactNode;
  footer?: ReactNode;
  writeError: unknown;
  children: ReactNode;
}) {
  const { t } = useLingui();
  const failed = query.error !== null && query.data === undefined;
  return (
    <Section id={id} icon={icon} title={title} description={description}>
      <InlineFailure error={writeError} />
      {failed ? (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="section.failed">
                This section could not be loaded
              </Trans>
            </SurfaceAlert.Title>
            <SurfaceAlert.Description>
              {t(describeError(query.error))}
            </SurfaceAlert.Description>
            <Button
              onClick={() => void query.refetch()}
              variant="destructive"
              size="sm"
              className="mt-2 w-fit"
            >
              <Trans id="section.retry">Try again</Trans>
            </Button>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      ) : (
        <ItemList
          label={label}
          loading={query.isPending}
          empty={empty}
          footer={footer}
        >
          {children}
        </ItemList>
      )}
    </Section>
  );
}
