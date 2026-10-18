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
 */
export function TotpSetup({
  secret,
  uri,
  split = false,
}: {
  secret: string;
  uri: string;
  /** Puts the QR code beside the setup key from `sm`, for a wide surface such as a dialog. */
  split?: boolean;
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
      <div
        className={
          split
            ? "flex min-w-0 flex-col gap-4 sm:flex-row sm:items-center sm:gap-6 sm:*:data-[slot=field]:min-w-0 sm:*:data-[slot=field]:flex-1 sm:[&_canvas]:shrink-0"
            : "contents"
        }
      >
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
          wrap
        />
      </div>
    </div>
  );
}
