import { Trans } from "@lingui/react/macro";
import { useState } from "react";
import type { RequestExchange } from "@/api/exchange";
import { Button } from "@/components/custom/Button";
import { Detail } from "@/components/custom/Detail";
import { ReadOnlyText } from "@/components/custom/ReadOnlyText";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";

/**
 * What a failed request sent and got back, opened from the error toast. Open
 * while `exchange` is set; it keeps the last one while it closes, so the
 * closing animation still has something to draw.
 */
export function RequestDetailsDialog({
  exchange,
  onClose,
}: {
  exchange: RequestExchange | null;
  onClose: () => void;
}) {
  const [shown, setShown] = useState<RequestExchange | null>(exchange);
  if (exchange !== null && exchange !== shown) setShown(exchange);

  const response = shown?.response;
  const headers = response?.headers
    .map(([name, value]) => `${name}: ${value}`)
    .join("\n");

  return (
    <Dialog
      open={exchange !== null}
      onOpenChange={(open) => !open && onClose()}
    >
      <DialogPopup
        className="sm:max-w-2xl"
        showCloseButton={false}
        portalProps={{ container: getTopLayerContainer() }}
      >
        <DialogHeader>
          <DialogTitle>
            <Trans id="request_details.title">Request details</Trans>
          </DialogTitle>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-4">
          {shown && (
            <dl className="flex min-w-0 flex-col gap-3">
              <Detail
                label={<Trans id="request_details.request">Request</Trans>}
              >
                <span className="font-mono">
                  {shown.method} {shown.path}
                </span>
              </Detail>
              {response && (
                <Detail
                  label={<Trans id="request_details.status">Status</Trans>}
                >
                  {response.status}
                </Detail>
              )}
            </dl>
          )}
          {shown && !response && (
            <p>
              <Trans id="request_details.no_response">
                No response was received from the server.
              </Trans>
            </p>
          )}
          {shown?.requestBody !== undefined && (
            <ReadOnlyText
              label={
                <Trans id="request_details.request_body">Request body</Trans>
              }
              value={shown.requestBody}
            />
          )}
          {headers && (
            <ReadOnlyText
              label={
                <Trans id="request_details.response_headers">
                  Response headers
                </Trans>
              }
              value={headers}
            />
          )}
          {response?.body !== undefined && (
            <ReadOnlyText
              label={
                <Trans id="request_details.response_body">Response body</Trans>
              }
              value={response.body}
            />
          )}
        </DialogPanel>
        <DialogFooter>
          <Button onClick={onClose}>
            <Trans id="request_details.close">Close</Trans>
          </Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}
