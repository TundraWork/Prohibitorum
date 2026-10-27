import { msg } from "@lingui/core/macro";
import { useLingui } from "@lingui/react/macro";
import { useBlocker } from "@tanstack/react-router";
import { CheckIcon, CopyIcon, DownloadIcon } from "lucide-react";
import {
  type ReactNode,
  type RefObject,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { breakAtSlashes, CopyStatus } from "@/components/custom/CopyableValue";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import type { SecretRevealCopy } from "@/components/custom/secret-reveal-copy";
import { useCopyValue } from "@/components/custom/use-copy-value";
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { cn } from "@/lib/utils";

interface RevealProps {
  text: string;
  filename: string;
  copy: SecretRevealCopy;
  onContinue: () => Promise<void>;
}

/**
 * The state every form shares: whether the secret has been taken (copied,
 * copied by hand or downloaded), the continue request, the confirmation shown
 * when leaving before it was, and the guard against navigating away.
 */
function useReveal({ onContinue }: Pick<RevealProps, "onContinue">) {
  const [taken, setTaken] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const continuing = useRef(false);
  // A navigation away asks through the same confirmation; only closing the
  // tab is left to the browser's own prompt, which cannot be styled.
  const blocker = useBlocker({
    shouldBlockFn: () => !taken && !continuing.current,
    enableBeforeUnload: () => !taken && !continuing.current,
    withResolver: true,
  });
  const navigating = blocker.status === "blocked";

  function leave() {
    if (continuing.current) return;
    continuing.current = true;
    setConfirming(false);
    setPending(true);
    void onContinue().catch(() => {
      continuing.current = false;
      setPending(false);
    });
  }

  /** Continue, or ask first when the secret was never copied or downloaded. */
  function proceed() {
    if (taken) leave();
    else setConfirming(true);
  }

  const take = useCallback(() => setTaken(true), []);
  return {
    take,
    pending,
    proceed,
    /** Whether the leave confirmation is showing, for Continue or a navigation. */
    confirming: confirming || navigating,
    /** Closing the confirmation stays, and cancels a held navigation. */
    stay: () => {
      setConfirming(false);
      if (navigating) blocker.reset();
    },
    /** Confirming it leaves the way it was asked: by Continue or by the navigation. */
    confirmLeave: () => {
      if (navigating) blocker.proceed();
      else leave();
    },
  };
}

type Reveal = ReturnType<typeof useReveal>;

const leaveTitleMessage = msg({
  id: "secret.leave.title",
  message: "Close without copying?",
});

function RevealBody({
  text,
  filename,
  copy,
  reveal,
  title,
}: Omit<RevealProps, "onContinue"> & { reveal: Reveal; title?: ReactNode }) {
  const { t } = useLingui();
  const { state, copy: copyValue, confirm } = useCopyValue();
  const { take } = reveal;
  useEffect(() => {
    if (state === "copied") take();
  }, [state, take]);
  const [downloadFailed, setDownloadFailed] = useState(false);
  const lines = text.split("\n");
  const list = lines.length > 1;

  function download() {
    setDownloadFailed(false);
    let url: string | undefined;
    try {
      url = URL.createObjectURL(
        new Blob([`${text}\n`], { type: "text/plain;charset=utf-8" }),
      );
      const link = document.createElement("a");
      link.href = url;
      link.download = filename;
      document.body.append(link);
      link.click();
      link.remove();
      take();
    } catch {
      setDownloadFailed(true);
    } finally {
      if (url) {
        const objectUrl = url;
        window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
      }
    }
  }

  const copied = state === "copied";
  return (
    <>
      {title}
      <figure
        data-slot="secret-well"
        className="flex flex-col gap-3 rounded-xl bg-muted/60 p-4 ring-1 ring-border/60"
      >
        <figcaption className="font-medium text-sm">{t(copy.label)}</figcaption>
        {list ? (
          <ul
            onCopy={confirm}
            className="grid gap-x-6 gap-y-1.5 font-mono text-[13px] leading-5 whitespace-nowrap select-all sm:grid-cols-2"
          >
            {lines.map((line) => (
              <li key={line}>{line}</li>
            ))}
          </ul>
        ) : (
          <p
            onCopy={confirm}
            className="font-mono text-[13px] leading-5 wrap-anywhere select-all"
          >
            {breakAtSlashes(text, { atomic: true })}
          </p>
        )}
        <div className="flex flex-wrap gap-2">
          <Button
            onClick={() => copyValue(text)}
            disabled={reveal.pending}
            variant="outline"
            size="sm"
          >
            {/* Both labels share one cell, so the button keeps the wider
                one's width and nothing beside it moves when it changes. */}
            <span className="grid justify-items-center">
              <span
                className={cn(
                  "col-start-1 row-start-1 inline-flex items-center gap-1.5",
                  copied && "invisible",
                )}
              >
                <CopyIcon aria-hidden="true" />
                {t(copy.copy)}
              </span>
              <span
                className={cn(
                  "col-start-1 row-start-1 inline-flex items-center gap-1.5",
                  !copied && "invisible",
                )}
              >
                <CheckIcon aria-hidden="true" className="text-success" />
                {t({ id: "copy-value.copied", message: "Copied" })}
              </span>
            </span>
          </Button>
          {copy.download !== undefined && (
            <Button
              onClick={download}
              disabled={reveal.pending}
              variant="ghost"
              size="sm"
            >
              <DownloadIcon aria-hidden="true" />
              {t(copy.download)}
            </Button>
          )}
        </div>
      </figure>
      {copy.usage !== undefined && (
        <p className="text-muted-foreground text-sm">{t(copy.usage)}</p>
      )}
      <CopyStatus state={state} failedMessage={t(copy.copyFailed)} />
      {downloadFailed && copy.downloadFailed !== undefined && (
        <SurfaceAlert status="warning" role="alert">
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(copy.downloadFailed)}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
    </>
  );
}

/** Asked when leaving before the secret was copied or downloaded. */
function LeaveConfirm({
  copy,
  reveal,
}: {
  copy: SecretRevealCopy;
  reveal: Reveal;
}) {
  const { t } = useLingui();
  return (
    <ConfirmDialog
      isOpen={reveal.confirming}
      onOpenChange={(open) => {
        if (!open) reveal.stay();
      }}
      status="warning"
      title={t(leaveTitleMessage)}
      body={t(copy.leave)}
      confirmLabel={t({ id: "secret.leave.confirm", message: "Close anyway" })}
      onConfirm={reveal.confirmLeave}
    />
  );
}

function ContinueButton({
  copy,
  reveal,
}: {
  copy: SecretRevealCopy;
  reveal: Reveal;
}) {
  const { t } = useLingui();
  return (
    <Button pending={reveal.pending} onClick={reveal.proceed}>
      {t(copy.continueLabel)}
    </Button>
  );
}

/**
 * One-time disclosure of a secret the server will never return again: a client
 * secret, a registration link, an access token or recovery codes. The value
 * sits in a well with its copy control (and, for recovery codes, download);
 * the reader leaves through Continue, which asks first while the value was
 * never copied or downloaded. The caller supplies the wording and the
 * filename; the rest is shared so every secret warns about the same loss.
 *
 * `titled={false}` is for a caller that draws the title as its own heading,
 * such as a step on the public card whose title is the page's `h1`.
 */
export function SecretReveal({
  text,
  filename,
  copy,
  onContinue,
  titled = true,
}: RevealProps & { titled?: boolean }) {
  const { t } = useLingui();
  const reveal = useReveal({ onContinue });
  return (
    <div className="flex flex-col gap-4">
      <RevealBody
        text={text}
        filename={filename}
        copy={copy}
        reveal={reveal}
        title={
          <>
            {titled && (
              <h2 className="text-xl font-semibold">{t(copy.title)}</h2>
            )}
            <p className="text-muted-foreground text-sm">{t(copy.once)}</p>
          </>
        }
      />
      <ContinueButton copy={copy} reveal={reveal} />
      <LeaveConfirm copy={copy} reveal={reveal} />
    </div>
  );
}

/**
 * The reveal as the last step of a dialog the caller owns: a header carrying
 * the caller's `progress` above the title, the secret in the panel, and
 * Continue in the footer. The caller routes its own Escape and close button
 * through `proceedRef`, so they ask before leaving exactly as Continue does.
 */
export function SecretRevealStep({
  text,
  filename,
  copy,
  onContinue,
  progress,
  proceedRef,
}: RevealProps & {
  progress?: ReactNode;
  /**
   * Receives what the Continue button does, asking first while the secret was
   * never copied or downloaded, so the caller's Escape and close button can
   * follow it as `SecretRevealDialog`'s do.
   */
  proceedRef?: RefObject<(() => void) | null>;
}) {
  const { t } = useLingui();
  const reveal = useReveal({ onContinue });
  const { proceed } = reveal;
  useEffect(() => {
    if (proceedRef) proceedRef.current = proceed;
  });
  return (
    <>
      <DialogHeader>
        {progress}
        <DialogTitle>{t(copy.title)}</DialogTitle>
        <DialogDescription>{t(copy.once)}</DialogDescription>
      </DialogHeader>
      <DialogPanel className="flex flex-col gap-4">
        <RevealBody
          text={text}
          filename={filename}
          copy={copy}
          reveal={reveal}
        />
      </DialogPanel>
      <DialogFooter>
        <ContinueButton copy={copy} reveal={reveal} />
      </DialogFooter>
      <LeaveConfirm copy={copy} reveal={reveal} />
    </>
  );
}

/**
 * The same reveal in a dialog that is open exactly while the caller holds the
 * secret, so it cannot outlive it. Escape and the close button do what
 * Continue does, asking first while the secret was never copied or
 * downloaded. A press outside never dismisses it.
 */
export function SecretRevealDialog({
  text,
  filename,
  copy,
  onContinue,
}: RevealProps) {
  const { t } = useLingui();
  const reveal = useReveal({ onContinue });
  return (
    <Dialog
      open
      disablePointerDismissal
      onOpenChange={(open) => {
        if (!open) reveal.proceed();
      }}
    >
      <DialogPopup className="sm:max-w-lg" showCloseButton>
        <DialogHeader>
          <DialogTitle>{t(copy.title)}</DialogTitle>
          <DialogDescription>{t(copy.once)}</DialogDescription>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-4">
          <RevealBody
            text={text}
            filename={filename}
            copy={copy}
            reveal={reveal}
          />
        </DialogPanel>
        <DialogFooter>
          <ContinueButton copy={copy} reveal={reveal} />
        </DialogFooter>
      </DialogPopup>
      <LeaveConfirm copy={copy} reveal={reveal} />
    </Dialog>
  );
}
