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

export function RecoveryCodes({
  codes,
  onContinue,
}: {
  codes: string[];
  onContinue: () => Promise<void>;
}) {
  const { t } = useLingui();
  const [saved, setSaved] = useState(false);
  const [copyState, setCopyState] = useState<"idle" | "copied" | "failed">(
    "idle",
  );
  const [downloadFailed, setDownloadFailed] = useState(false);
  const [pending, setPending] = useState(false);
  const continuing = useRef(false);
  const text = codes.join("\n");
  useBlocker({
    shouldBlockFn: () =>
      !saved &&
      !continuing.current &&
      !window.confirm(
        t({
          id: "login.codes.leave",
          message:
            "Leave without saving? These recovery codes cannot be shown again.",
        }),
      ),
    enableBeforeUnload: () => !saved && !continuing.current,
  });

  async function copy() {
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
      link.download = "prohibitorum-recovery-codes.txt";
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
      <h2 className="text-xl font-semibold">
        <Trans id="login.codes.title">Save your new recovery codes</Trans>
      </h2>
      <Alert status="warning">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>
            <Trans id="login.codes.once">
              These codes are shown only once. Keep them somewhere safe before
              continuing. Your old recovery codes no longer work.
            </Trans>
          </Alert.Title>
        </Alert.Content>
      </Alert>
      <TextField isReadOnly value={text}>
        <Label>
          <Trans id="login.codes.label">New recovery codes</Trans>
        </Label>
        <TextArea
          rows={codes.length}
          autoComplete="off"
          spellCheck={false}
          className="font-mono"
        />
      </TextField>
      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          onPress={() => {
            void copy();
          }}
          isDisabled={pending}
        >
          <Trans id="login.codes.copy">Copy codes</Trans>
        </Button>
        <Button variant="secondary" onPress={download} isDisabled={pending}>
          <Trans id="login.codes.download">Download codes</Trans>
        </Button>
      </div>
      {copyState === "copied" && (
        <p role="status">
          <Trans id="login.codes.copied">Codes copied.</Trans>
        </p>
      )}
      {copyState === "failed" && (
        <Alert status="warning" role="alert">
          <Alert.Content>
            <Alert.Title>
              <Trans id="login.codes.copy_failed">
                Could not copy the codes. Select the codes above and copy them
                manually, or download them.
              </Trans>
            </Alert.Title>
          </Alert.Content>
        </Alert>
      )}
      {downloadFailed && (
        <Alert status="warning" role="alert">
          <Alert.Content>
            <Alert.Title>
              <Trans id="login.codes.download_failed">
                Could not download the codes. Copy them or save them manually.
              </Trans>
            </Alert.Title>
          </Alert.Content>
        </Alert>
      )}
      <Checkbox isSelected={saved} onChange={setSaved} isDisabled={pending}>
        <Checkbox.Content>
          <Checkbox.Control>
            <Checkbox.Indicator />
          </Checkbox.Control>
          <Trans id="login.codes.saved">I have saved my recovery codes</Trans>
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
        <Trans id="login.codes.continue">Continue</Trans>
      </Button>
    </div>
  );
}
