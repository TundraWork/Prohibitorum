import { useLingui } from "@lingui/react/macro";
import { useCallback, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * The failure a page draws itself for a write it made.
 *
 * `report` keeps the failure unless it is a cancellation: backing out of the
 * browser's passkey prompt or the identity check is a decision, not an error,
 * so there is nothing to say. `clear` drops it when the reader tries again.
 * Pair it with `withInlineReport` so the toast does not say it twice.
 */
export function useInlineFailure() {
  const [error, setError] = useState<unknown>(null);
  const clear = useCallback(() => setError(null), []);
  const report = useCallback((failure: unknown) => {
    if (!isCancellation(failure)) setError(failure);
  }, []);
  return { error, clear, report };
}

/** Says what went wrong in words, or nothing while there is no failure. */
export function InlineFailure({ error }: { error: unknown }) {
  const { t } = useLingui();
  if (error === null) return null;
  return (
    <SurfaceAlert status="danger" role="alert">
      <SurfaceAlert.Indicator />
      <SurfaceAlert.Content>
        <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
      </SurfaceAlert.Content>
    </SurfaceAlert>
  );
}
