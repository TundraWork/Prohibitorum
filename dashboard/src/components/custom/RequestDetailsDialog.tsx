import { Label, Modal, TextArea, TextField } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { type ReactNode, useState } from "react";
import type { RequestExchange } from "@/api/exchange";
import { Button } from "@/components/custom/Button";
import { Detail } from "@/components/custom/Detail";

/** Taller text than this scrolls inside its box. */
const maxRows = 12;

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
    <Modal
      isOpen={exchange !== null}
      onOpenChange={(open) => !open && onClose()}
    >
      <Modal.Backdrop>
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>
                <Trans id="request_details.title">Request details</Trans>
              </Modal.Heading>
            </Modal.Header>
            <Modal.Body className="flex flex-col gap-4">
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
                <ReadOnlyBlock
                  label={
                    <Trans id="request_details.request_body">
                      Request body
                    </Trans>
                  }
                  value={shown.requestBody}
                />
              )}
              {headers && (
                <ReadOnlyBlock
                  label={
                    <Trans id="request_details.response_headers">
                      Response headers
                    </Trans>
                  }
                  value={headers}
                />
              )}
              {response?.body !== undefined && (
                <ReadOnlyBlock
                  label={
                    <Trans id="request_details.response_body">
                      Response body
                    </Trans>
                  }
                  value={response.body}
                />
              )}
            </Modal.Body>
            <Modal.Footer>
              <Button onPress={onClose}>
                <Trans id="request_details.close">Close</Trans>
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

/** Text to read and copy from, in a box that scrolls past `maxRows` lines. */
function ReadOnlyBlock({ label, value }: { label: ReactNode; value: string }) {
  return (
    <TextField isReadOnly value={value}>
      <Label>{label}</Label>
      <TextArea
        rows={Math.min(value.split("\n").length, maxRows)}
        variant="secondary"
        spellCheck={false}
        className="font-mono text-xs"
      />
    </TextField>
  );
}
