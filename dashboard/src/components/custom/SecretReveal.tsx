import { Modal } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useBlocker } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { Button } from "@/components/custom/Button";
import { ReadOnlyText } from "@/components/custom/ReadOnlyText";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import type { SecretRevealCopy } from "@/components/custom/secret-reveal-copy";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

/**
 * One-time disclosure of a secret the server will never return again: recovery
 * codes and personal access token plaintext. The caller supplies the wording
 * and the filename; the leaving guard, copy and download behaviour is shared so
 * both secrets warn about the same irreversible loss.
 *
 * `onSurface` is kept so a caller can say where it draws the reveal; coss's
 * alert and fields read the same on a page, a card and a dialog, so it no
 * longer changes the look.
 *
 * `inDialog` is for a caller's dialog, drawn by that caller: the title goes in
 * its header, and the continue control becomes the dialog's footer so it stays
 * put under a long code list. While callers still use HeroUI modals the two
 * sections are that modal's `Body` and `Footer`.
 *
 * `titled={false}` is for a caller that draws the title as its own heading,
 * such as a step on the public card whose title is the page's `h1`.
 */
export function SecretReveal({
  text,
  filename,
  copy,
  onContinue,
  inDialog = false,
  titled = true,
}: {
  text: string;
  filename: string;
  copy: SecretRevealCopy;
  onContinue: () => Promise<void>;
  /** Accepted for compatibility; see above. */
  onSurface?: boolean;
  inDialog?: boolean;
  titled?: boolean;
}) {
  const { t } = useLingui();
  const [saved, setSaved] = useState(false);
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const [downloadFailed, setDownloadFailed] = useState(false);
  const [pending, setPending] = useState(false);
  const continuing = useRef(false);
  useBlocker({
    shouldBlockFn: () =>
      !saved && !continuing.current && !window.confirm(t(copy.leave)),
    enableBeforeUnload: () => !saved && !continuing.current,
  });

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

  const body = (
    <>
      {!inDialog && titled && (
        <h2 className="text-xl font-semibold">{t(copy.title)}</h2>
      )}
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
          variant="secondary"
          onPress={() => {
            void copyText();
          }}
          isDisabled={pending}
        >
          {t(copy.copy)}
        </Button>
        <Button variant="secondary" onPress={download} isDisabled={pending}>
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
          checked={saved}
          onCheckedChange={setSaved}
          disabled={pending}
        />
        {t(copy.saved)}
      </Label>
    </>
  );
  const action = (
    <Button
      isDisabled={!saved}
      isPending={pending}
      onPress={() => {
        if (!saved || continuing.current) return;
        continuing.current = true;
        setPending(true);
        void onContinue().catch(() => {
          continuing.current = false;
          setPending(false);
        });
      }}
    >
      {t(copy.continueLabel)}
    </Button>
  );

  if (inDialog) {
    return (
      <>
        <Modal.Body className="flex flex-col gap-4">{body}</Modal.Body>
        <Modal.Footer>{action}</Modal.Footer>
      </>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {body}
      {action}
    </div>
  );
}
