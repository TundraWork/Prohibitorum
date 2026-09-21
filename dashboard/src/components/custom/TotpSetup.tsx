import { Alert, Input, Label, TextField } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import QRCode from "qrcode";
import { useEffect, useRef, useState } from "react";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * The locally generated authenticator secret, its otpauth URI, and the QR code
 * that carries them.
 *
 * `onSurface` follows where the caller draws it. On a surface — a Card or a
 * Dialog — the alert and the setup key drop their own background and shadow,
 * because the surface already carries that plane: the sign-in reset inside a
 * card passes it, while the console draws the setup on the page background.
 */
export function TotpSetup({
  secret,
  uri,
  onSurface = false,
}: {
  secret: string;
  uri: string;
  onSurface?: boolean;
}) {
  const Notice = onSurface ? SurfaceAlert : Alert;
  const { t } = useLingui();
  const canvas = useRef<HTMLCanvasElement>(null);
  const [qrFailed, setQrFailed] = useState(false);

  // The canvas stays mounted across failures, so this effect can draw on it
  // again the moment `uri` changes. Unmounting it (or skipping the empty uri
  // that the first render passes while the config query resolves) would leave
  // the ref null and the canvas blank forever.
  useEffect(() => {
    let active = true;
    setQrFailed(false);
    const element = canvas.current;
    if (uri === "" || !element) return;
    void QRCode.toCanvas(element, uri, {
      width: 240,
      margin: 4,
    }).catch(() => {
      if (active) setQrFailed(true);
    });
    return () => {
      active = false;
    };
  }, [uri]);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Notice status="warning">
        <Notice.Indicator />
        <Notice.Content>
          <Notice.Title>
            <Trans id="login.reset.warning">
              After a successful reset, your old authenticator and all old
              recovery codes will stop working.
            </Trans>
          </Notice.Title>
        </Notice.Content>
      </Notice>
      <p className="text-sm text-muted">
        <Trans id="login.reset.scan">
          Scan this QR code with your authenticator, or enter the setup key
          manually. Then enter a code from the new authenticator.
        </Trans>
      </p>
      <canvas
        ref={canvas}
        className={qrFailed ? "hidden" : "max-w-full self-center"}
        role="img"
        aria-label={t({
          id: "login.reset.qr",
          message: "New authenticator setup QR code",
        })}
      />
      {qrFailed && (
        <Notice status="warning">
          <Notice.Content>
            <Notice.Title>
              <Trans id="login.reset.qr_failed">
                The QR code could not be displayed. Enter the setup key
                manually.
              </Trans>
            </Notice.Title>
          </Notice.Content>
        </Notice>
      )}
      <TextField isReadOnly value={secret}>
        <Label>
          <Trans id="login.reset.secret">Setup key</Trans>
        </Label>
        <Input
          variant={onSurface ? "secondary" : "primary"}
          autoComplete="off"
          spellCheck={false}
        />
      </TextField>
    </div>
  );
}
