import { Toast, ToastQueue } from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { Trans, useLingui } from "@lingui/react/macro";
import { type ReactNode, useEffect, useState } from "react";
import {
  ApiError,
  describeError,
  type ErrorDescription,
  type ErrorScope,
  isCancellation,
} from "@/api/errors";
import type { RequestExchange } from "@/api/exchange";
import { RequestDetailsDialog } from "@/components/custom/RequestDetailsDialog";

type Notification =
  | { title: ReactNode; error?: never; exchange?: never; success?: never }
  | {
      error: ErrorDescription;
      exchange?: RequestExchange;
      title?: never;
      success?: never;
    }
  | {
      success: MessageDescriptor;
      title?: never;
      error?: never;
      exchange?: never;
    };

export const notificationQueue = new ToastQueue<Notification>();

/**
 * How long a success stays up. An error closes after the library's default.
 * Either one holds while the pointer or focus is on it, which is the time to
 * open an error's details.
 */
const successTimeout = 5000;

export function notifyError(error: unknown, scope?: ErrorScope) {
  if (!isCancellation(error)) {
    notificationQueue.add({
      error: describeError(error, scope),
      exchange: error instanceof ApiError ? error.exchange : undefined,
    });
  }
}

/**
 * Says that a write went through. The message is a descriptor rather than
 * text, so a toast still on screen follows a change of language.
 */
export function notifySuccess(message: MessageDescriptor) {
  notificationQueue.add({ success: message }, { timeout: successTimeout });
}

export function AppNotifications() {
  const { i18n, t } = useLingui();
  const [details, setDetails] = useState<RequestExchange | null>(null);
  useEffect(() => () => notificationQueue.clear(), []);

  return (
    <>
      <Toast.Provider
        queue={notificationQueue}
        placement="bottom end"
        aria-label={t({ id: "notification.region", message: "Notifications" })}
      >
        {({ toast }) => (
          <Toast
            toast={toast}
            variant={
              toast.content.error
                ? "danger"
                : toast.content.success
                  ? "success"
                  : "default"
            }
          >
            {toast.content.success && <Toast.Indicator variant="success" />}
            <Toast.Content>
              <Toast.Title>
                {toast.content.error
                  ? i18n._(toast.content.error)
                  : toast.content.success
                    ? i18n._(toast.content.success)
                    : toast.content.title}
              </Toast.Title>
              {toast.content.error?.requestId && (
                <Toast.Description>
                  <Trans id="notification.request_id">
                    Request ID: {toast.content.error.requestId}
                  </Trans>
                </Toast.Description>
              )}
            </Toast.Content>
            {toast.content.exchange && (
              <Toast.ActionButton
                onPress={() => setDetails(toast.content.exchange ?? null)}
              >
                <Trans id="notification.details">Details</Trans>
              </Toast.ActionButton>
            )}
            <Toast.CloseButton
              aria-label={t({
                id: "notification.close",
                message: "Close notification",
              })}
            />
          </Toast>
        )}
      </Toast.Provider>
      <RequestDetailsDialog
        exchange={details}
        onClose={() => setDetails(null)}
      />
    </>
  );
}
