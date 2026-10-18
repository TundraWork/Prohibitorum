import { Trans, useLingui } from "@lingui/react/macro";
import { useBlocker } from "@tanstack/react-router";
import {
  type ReactNode,
  type RefObject,
  useEffect,
  useRef,
  useState,
} from "react";
import { Button } from "@/components/custom/Button";
import { ReadOnlyText } from "@/components/custom/ReadOnlyText";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import type { SecretRevealCopy } from "@/components/custom/secret-reveal-copy";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";

interface RevealProps {
  text: string;
  filename: string;
  copy: SecretRevealCopy;
  onContinue: () => Promise<void>;
}

/**
 * The state both forms share: whether the secret is confirmed saved, the
 * continue request, and the guard against leaving the page before it is.
 */
function useReveal({
  copy,
  onContinue,
}: Pick<RevealProps, "copy" | "onContinue">) {
  const { t } = useLingui();
  const [saved, setSaved] = useState(false);
  const [pending, setPending] = useState(false);
  const continuing = useRef(false);
  useBlocker({
    shouldBlockFn: () =>
      !saved && !continuing.current && !window.confirm(t(copy.leave)),
    enableBeforeUnload: () => !saved && !continuing.current,
  });

  function proceed() {
    if (!saved || continuing.current) return;
    continuing.current = true;
    setPending(true);
    void onContinue().catch(() => {
      continuing.current = false;
      setPending(false);
    });
  }

  return { saved, setSaved, pending, proceed };
}

type Reveal = ReturnType<typeof useReveal>;

function RevealBody({
  text,
  filename,
  copy,
  reveal,
  title,
}: Omit<RevealProps, "onContinue"> & { reveal: Reveal; title?: ReactNode }) {
  const { t } = useLingui();
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const [downloadFailed, setDownloadFailed] = useState(false);

  async function copyText() {
    setCopyState("idle");
    try {
      await navigator.clipboard.writeText(text);
      setCopyState("copied");
    } catch {
      setCopyState("failed");
    }
  }

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
    } catch {
      setDownloadFailed(true);
    } finally {
      if (url) {
        const objectUrl = url;
        window.setTimeout(() => URL.revokeObjectURL(objectUrl), 0);
      }
    }
  }

  return (
    <>
      {title}
      <SurfaceAlert status="warning">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>{t(copy.once)}</SurfaceAlert.Title>
        </SurfaceAlert.Content>
      </SurfaceAlert>
      <ReadOnlyText
        label={t(copy.label)}
        description={copy.usage !== undefined ? t(copy.usage) : undefined}
        value={text}
        maxRows={Number.POSITIVE_INFINITY}
      />
      <div className="flex flex-wrap gap-2">
        <Button
          onClick={() => {
            void copyText();
          }}
          disabled={reveal.pending}
          variant="outline"
          size="lg"
        >
          {t(copy.copy)}
        </Button>
        <Button
          onClick={download}
          disabled={reveal.pending}
          variant="outline"
          size="lg"
        >
          {t(copy.download)}
        </Button>
      </div>
      {copyState === "copied" && (
        <p role="status">
          <Trans id="secret.copied">Copied.</Trans>
        </p>
      )}
      {copyState === "failed" && (
        <SurfaceAlert status="warning" role="alert">
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(copy.copyFailed)}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
      {downloadFailed && (
        <SurfaceAlert status="warning" role="alert">
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(copy.downloadFailed)}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
      <Label className="w-fit">
        <Checkbox
          checked={reveal.saved}
          onCheckedChange={reveal.setSaved}
          disabled={reveal.pending}
        />
        {t(copy.saved)}
      </Label>
    </>
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
    <Button
      disabled={!reveal.saved}
      pending={reveal.pending}
      onClick={reveal.proceed}
    >
      {t(copy.continueLabel)}
    </Button>
  );
}

/**
 * One-time disclosure of a secret the server will never return again: recovery
 * codes and personal access token plaintext. The caller supplies the wording
 * and the filename; the leaving guard, copy and download behaviour is shared so
 * both secrets warn about the same irreversible loss.
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
  const reveal = useReveal({ copy, onContinue });
  return (
    <div className="flex flex-col gap-4">
      <RevealBody
        text={text}
        filename={filename}
        copy={copy}
        reveal={reveal}
        title={
          titled ? (
            <h2 className="text-xl font-semibold">{t(copy.title)}</h2>
          ) : undefined
        }
      />
      <ContinueButton copy={copy} reveal={reveal} />
    </div>
  );
}

/**
 * The reveal as the last step of a dialog the caller owns: a header carrying
 * the caller's `progress` above the title, the secret in the panel, and
 * Continue in the footer. The caller keeps the dialog open and unclosable
 * while it shows this, since the server will never show the secret again.
 */
export function SecretRevealStep({
  text,
  filename,
  copy,
  onContinue,
  progress,
  proceedRef,
  onSavedChange,
}: RevealProps & {
  progress?: ReactNode;
  /** Told whether the secret is confirmed saved, for a caller that shows its own close control. */
  onSavedChange?: (saved: boolean) => void;
  /**
   * Receives the same "leave" the Continue button performs, which does nothing
   * until the secret is confirmed saved, so the caller's Escape and close
   * button can follow it as `SecretRevealDialog`'s do.
   */
  proceedRef?: RefObject<(() => void) | null>;
}) {
  const { t } = useLingui();
  const reveal = useReveal({ copy, onContinue });
  const { proceed } = reveal;
  useEffect(() => {
    if (proceedRef) proceedRef.current = proceed;
  });
  const { saved } = reveal;
  useEffect(() => {
    onSavedChange?.(saved);
    return () => onSavedChange?.(false);
  }, [saved, onSavedChange]);
  return (
    <>
      <DialogHeader>
        {progress}
        <DialogTitle>{t(copy.title)}</DialogTitle>
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
    </>
  );
}

/**
 * The same reveal in a dialog that is open exactly while the caller holds the
 * secret, so it cannot outlive it. Escape does
 * nothing until the secret is confirmed saved, because the server will never
 * show it again, and the close button is not shown until then; after that,
 * closing is the same as continuing. Pointer
 * dismissal (a press outside) is disabled throughout.
 */
export function SecretRevealDialog({
  text,
  filename,
  copy,
  onContinue,
}: RevealProps) {
  const { t } = useLingui();
  const reveal = useReveal({ copy, onContinue });
  return (
    <Dialog
      open
      disablePointerDismissal
      onOpenChange={(open) => {
        if (!open) reveal.proceed();
      }}
    >
      <DialogPopup
        className="sm:max-w-lg"
        // The close button would do nothing until the secret is saved.
        showCloseButton={reveal.saved}
        portalProps={{ container: getTopLayerContainer() }}
      >
        <DialogHeader>
          <DialogTitle>{t(copy.title)}</DialogTitle>
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
    </Dialog>
  );
}
