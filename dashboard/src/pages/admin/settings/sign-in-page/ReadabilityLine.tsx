import { Trans } from "@lingui/react/macro";
import { CheckIcon, EyeOffIcon, TriangleAlertIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import type { LoginAppearance } from "@/api/raw-paths";
import { Badge } from "@/components/ui/badge";
import {
  canvasEnvironment,
  type Environment,
  measureReadability,
  type ReadabilityResult,
  watchPreview,
} from "@/pages/admin/settings/sign-in-page/readability";

/**
 * How readable the card's text is over what is behind it in the preview, kept
 * current as the background, the card's surface, its place and the preview's
 * theme and size change. Nothing until the first measurement; the previous
 * answer stays while the next is worked out.
 */
export function useReadability(
  doc: Document | null,
  appearance: LoginAppearance,
  environment: Environment = canvasEnvironment,
): ReadabilityResult | undefined {
  const [result, setResult] = useState<ReadabilityResult>();
  const latest = useRef(appearance);
  latest.current = appearance;
  // The settings, as text: a settings object is rebuilt on every edit.
  const settings = JSON.stringify(appearance);

  // biome-ignore lint/correctness/useExhaustiveDependencies: the text stands for the settings.
  useEffect(() => {
    if (!doc) {
      setResult(undefined);
      return;
    }
    let live = true;
    let latestRun = 0;
    const measure = () => {
      const run = ++latestRun;
      void measureReadability(doc, latest.current, environment)
        .catch(
          (): ReadabilityResult => ({
            kind: "unmeasurable",
            cause: "page",
          }),
        )
        .then((next) => {
          if (live && run === latestRun) setResult(next);
        });
    };
    // The page the settings drew reaches the frame a moment after the edit.
    const settle = window.setTimeout(measure, 200);
    const stop = watchPreview(doc, measure);
    return () => {
      live = false;
      window.clearTimeout(settle);
      stop();
    };
  }, [doc, settings, environment]);

  return result;
}

/** Floored to a tenth, so a ratio just under the line never reads as on it. */
function formatRatio(ratio: number): string {
  return (Math.floor(ratio * 10) / 10).toFixed(1);
}

/** The verdict for the card section's footer. */
export function ReadabilityLine({ result }: { result: ReadabilityResult }) {
  return (
    <div role="status" className="flex flex-wrap items-center gap-x-2 gap-y-1">
      {result.kind === "unmeasurable" ? (
        <>
          <Badge variant="outline">
            <EyeOffIcon aria-hidden="true" />
            <Trans id="settings.sign-in.readability.not_measured">
              Not measured
            </Trans>
          </Badge>
          <span>
            {result.cause === "background" ? (
              <Trans id="settings.sign-in.readability.unmeasurable">
                Readability can't be measured for this background.
              </Trans>
            ) : (
              <Trans id="settings.sign-in.readability.unmeasurable_now">
                Readability can't be measured right now.
              </Trans>
            )}
          </span>
        </>
      ) : (
        <Verdict ratio={formatRatio(result.ratio)} readable={result.readable} />
      )}
    </div>
  );
}

function Verdict({ ratio, readable }: { ratio: string; readable: boolean }) {
  return readable ? (
    <>
      <Badge variant="success">
        <CheckIcon aria-hidden="true" />
        <Trans id="settings.sign-in.readability.readable">Readable</Trans>
      </Badge>
      <span>
        <Trans id="settings.sign-in.readability.contrast">
          Body text contrast {ratio} : 1
        </Trans>
      </span>
    </>
  ) : (
    <>
      <Badge variant="warning">
        <TriangleAlertIcon aria-hidden="true" />
        <Trans id="settings.sign-in.readability.hard">Hard to read</Trans>
      </Badge>
      <span>
        <Trans id="settings.sign-in.readability.contrast_hint">
          Body text contrast {ratio} : 1 — raise the card's opacity or pick
          another background
        </Trans>
      </span>
    </>
  );
}
