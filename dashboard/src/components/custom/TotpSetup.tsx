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
 * `onSurface` is kept so a caller can say where it draws the setup; coss's
 * alert and fields read the same on a page, a card and a dialog, so it no
 * longer changes the look.
 */
export function TotpSetup({
  secret,
  uri,
}: {
  secret: string;
  uri: string;
  /** Accepted for compatibility; see above. */
  onSurface?: boolean;
}) {
  const { t } = useLingui();

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        <Trans id="login.reset.scan">
          Scan this QR code with your authenticator, or enter the setup key
          manually. Then enter a code from the new authenticator.
        </Trans>
      </p>
      <QrCode
        value={uri}
        size={200}
        label={t({
          id: "login.reset.qr",
          message: "New authenticator setup QR code",
        })}
        failure={
          <SurfaceAlert status="warning">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="login.reset.qr_failed">
                  The QR code could not be displayed. Enter the setup key
                  manually.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        }
      />
      <CopyValue
        value={secret}
        label={<Trans id="login.reset.secret">Setup key</Trans>}
      />
    </div>
  );
}
