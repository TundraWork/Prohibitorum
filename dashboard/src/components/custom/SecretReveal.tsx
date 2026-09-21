import {
  Alert,
  Button,
  Checkbox,
  Label,
  Spinner,
  TextArea,
  TextField,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useBlocker } from "@tanstack/react-router";
import { useRef, useState } from "react";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import type { SecretRevealCopy } from "@/components/custom/secret-reveal-copy";

/**
 * One-time disclosure of a secret the server will never return again: recovery
 * codes and personal access token plaintext. The caller supplies the wording
 * and the filename; the leaving guard, copy and download behaviour is shared so
 * both secrets warn about the same irreversible loss.
 *
 * `onSurface` follows where the caller draws it: the console shows the reveal
 * on the page background, while the sign-in reset shows it inside the card.
 */
export function SecretReveal({
  text,
  filename,
  copy,
  onContinue,
  onSurface = false,
}: {
  text: string;
  filename: string;
  copy: SecretRevealCopy;
  onContinue: () => Promise<void>;
  onSurface?: boolean;
}) {
  const Notice = onSurface ? SurfaceAlert : Alert;
  const { t } = useLingui();
  const [saved, setSaved] = useState(false);
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const [downloadFailed, setDownloadFailed] = useState(false);
  const [pending, setPending] = useState(false);
  const continuing = useRef(false);
  const rows = text.split("\n").length;
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

  return (
    <div className="flex flex-col gap-4">
      <h2 className="text-xl font-semibold">{t(copy.title)}</h2>
      <Notice status="warning">
        <Notice.Indicator />
        <Notice.Content>
          <Notice.Title>{t(copy.once)}</Notice.Title>
        </Notice.Content>
      </Notice>
      <TextField isReadOnly value={text}>
        <Label>{t(copy.label)}</Label>
        <TextArea
          rows={rows}
          autoComplete="off"
          spellCheck={false}
          className="font-mono"
        />
      </TextField>
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
        <Notice status="warning" role="alert">
          <Notice.Content>
            <Notice.Title>{t(copy.copyFailed)}</Notice.Title>
          </Notice.Content>
        </Notice>
      )}
      {downloadFailed && (
        <Notice status="warning" role="alert">
          <Notice.Content>
            <Notice.Title>{t(copy.downloadFailed)}</Notice.Title>
          </Notice.Content>
        </Notice>
      )}
      <Checkbox isSelected={saved} onChange={setSaved} isDisabled={pending}>
        <Checkbox.Content>
          <Checkbox.Control>
            <Checkbox.Indicator />
          </Checkbox.Control>
          {t(copy.saved)}
        </Checkbox.Content>
      </Checkbox>
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
        {pending && <Spinner size="sm" color="current" />}
        {t(copy.continueLabel)}
      </Button>
    </div>
  );
}
