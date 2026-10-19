import { Trans } from "@lingui/react/macro";
import { CheckIcon, CopyIcon } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import { Button } from "@/components/custom/Button";
import { CopyStatus } from "@/components/custom/CopyableValue";
import { ReadOnlyText } from "@/components/custom/ReadOnlyText";
import { getTopLayerContainer } from "@/components/custom/top-layer";
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

type SigningKey = components["schemas"]["SigningKeyView"];

/**
 * The public half of a key as the JWKS publishes it, open while `signingKey`
 * is set. Nothing here is secret, so it is a plain dialog with a copy action
 * rather than a one-time reveal. It keeps the last key while it closes, so the
 * closing animation still has something to draw.
 */
export function PublicKeyDialog({
  signingKey,
  onClose,
}: {
  signingKey: SigningKey | null;
  onClose: () => void;
}) {
  const [shown, setShown] = useState<SigningKey | null>(signingKey);
  if (signingKey !== null && signingKey !== shown) setShown(signingKey);

  return (
    <Dialog
      open={signingKey !== null}
      onOpenChange={(open) => !open && onClose()}
    >
      <DialogPopup
        className="sm:max-w-xl"
        showCloseButton={false}
        portalProps={{ container: getTopLayerContainer() }}
      >
        <DialogHeader>
          <DialogTitle>
            <Trans id="settings.keys.jwk.title">Public key</Trans>
          </DialogTitle>
          <DialogDescription>
            <Trans id="settings.keys.jwk.description">
              As the instance publishes it for verifying tokens and assertions.
            </Trans>
          </DialogDescription>
        </DialogHeader>
        {shown !== null && (
          <PublicKeyBody key={shown.kid} signingKey={shown} onClose={onClose} />
        )}
      </DialogPopup>
    </Dialog>
  );
}

/** Remounted per key and dropped on close, so a copy state never outlives its key. */
function PublicKeyBody({
  signingKey,
  onClose,
}: {
  signingKey: SigningKey;
  onClose: () => void;
}) {
  const { state, copy } = useCopyValue();
  const jwk = JSON.stringify(signingKey.publicJwk, null, 2);
  return (
    <>
      <DialogPanel className="flex flex-col gap-3">
        <ReadOnlyText
          label={<Trans id="settings.keys.jwk.label">JSON Web Key</Trans>}
          value={jwk}
        />
        <CopyStatus state={state} />
      </DialogPanel>
      <DialogFooter>
        <Button variant="ghost" onClick={() => copy(jwk)}>
          {state === "copied" ? (
            <>
              <CheckIcon aria-hidden="true" className="text-success" />
              <Trans id="settings.keys.jwk.copied">Copied</Trans>
            </>
          ) : (
            <>
              <CopyIcon aria-hidden="true" />
              <Trans id="settings.keys.jwk.copy">Copy</Trans>
            </>
          )}
        </Button>
        <Button onClick={onClose}>
          <Trans id="settings.keys.jwk.close">Close</Trans>
        </Button>
      </DialogFooter>
    </>
  );
}
