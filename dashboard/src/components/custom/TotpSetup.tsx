import { Alert } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { CopyValue } from "@/components/custom/CopyValue";
import { QrCode } from "@/components/custom/QrCode";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * The locally generated authenticator secret, its otpauth URI, and the QR code
 * that carries them.
 *
 * It says nothing about what the new authenticator replaces: a first setup
 * replaces nothing, so a caller that does replace one — the sign-in reset, the
 * console's replace dialog — draws that warning itself.
 *
 * `onSurface` follows where the caller draws it. On a surface — a Card or a
 * Dialog — the QR failure notice drops its own background and shadow, because
 * the surface already carries that plane. The setup key is a `CopyValue`,
 * which is drawn for a surface already.
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

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <p className="text-sm text-muted">
        <Trans id="login.reset.scan">
          Scan this QR code with your authenticator, or enter the setup key
          manually. Then enter a code from the new authenticator.
        </Trans>
      </p>
      <QrCode
        value={uri}
        label={t({
          id: "login.reset.qr",
          message: "New authenticator setup QR code",
        })}
        failure={
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
        }
      />
      <CopyValue
        value={secret}
        label={<Trans id="login.reset.secret">Setup key</Trans>}
      />
    </div>
  );
}
