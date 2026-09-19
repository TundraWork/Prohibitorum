import { Alert, Input, Label, TextField } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import QRCode from "qrcode";
import { useEffect, useRef, useState } from "react";

export function TotpSetup({ secret, uri }: { secret: string; uri: string }) {
  const { t } = useLingui();
  const canvas = useRef<HTMLCanvasElement>(null);
  const [qrFailed, setQrFailed] = useState(false);

  useEffect(() => {
    let active = true;
    setQrFailed(false);
    if (canvas.current) {
      void QRCode.toCanvas(canvas.current, uri, {
        width: 240,
        margin: 4,
      }).catch(() => {
        if (active) setQrFailed(true);
      });
    }
    return () => {
      active = false;
    };
  }, [uri]);

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <Alert status="warning">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>
            <Trans id="login.reset.warning">
              After a successful reset, your old authenticator and all old
              recovery codes will stop working.
            </Trans>
          </Alert.Title>
        </Alert.Content>
      </Alert>
      <p className="text-sm text-muted">
        <Trans id="login.reset.scan">
          Scan this QR code with your authenticator, or enter the setup key
          manually. Then enter a code from the new authenticator.
        </Trans>
      </p>
      {!qrFailed && (
        <canvas
          ref={canvas}
          className="max-w-full self-center"
          role="img"
          aria-label={t({
            id: "login.reset.qr",
            message: "New authenticator setup QR code",
          })}
        />
      )}
      {qrFailed && (
        <Alert status="warning">
          <Alert.Content>
            <Alert.Title>
              <Trans id="login.reset.qr_failed">
                The QR code could not be displayed. Enter the setup key
                manually.
              </Trans>
            </Alert.Title>
          </Alert.Content>
        </Alert>
      )}
      <TextField isReadOnly value={secret}>
        <Label>
          <Trans id="login.reset.secret">Setup key</Trans>
        </Label>
        <Input autoComplete="off" spellCheck={false} />
      </TextField>
    </div>
  );
}
