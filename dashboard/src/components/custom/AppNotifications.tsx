import { Toast, ToastQueue } from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { Trans, useLingui } from "@lingui/react/macro";
import { type ReactNode, useEffect } from "react";
import {
  describeError,
  type ErrorDescription,
  type ErrorScope,
  isCancellation,
} from "@/api/errors";

type Notification =
  | { title: ReactNode; error?: never; success?: never }
  | { error: ErrorDescription; title?: never; success?: never }
  | { success: MessageDescriptor; title?: never; error?: never };

export const notificationQueue = new ToastQueue<Notification>();

/** How long a success stays up; an error stays until it is closed. */
const successTimeout = 5000;

export function notifyError(error: unknown, scope?: ErrorScope) {
  if (!isCancellation(error)) {
    notificationQueue.add({ error: describeError(error, scope) });
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
  useEffect(() => () => notificationQueue.clear(), []);

  return (
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
          <Toast.CloseButton
            aria-label={t({
              id: "notification.close",
              message: "Close notification",
            })}
          />
        </Toast>
      )}
    </Toast.Provider>
  );
}
