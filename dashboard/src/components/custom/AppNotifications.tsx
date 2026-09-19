import { Toast, ToastQueue } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { type ReactNode, useEffect } from "react";
import {
  describeError,
  type ErrorDescription,
  isCancellation,
} from "@/api/errors";

type Notification =
  | { title: ReactNode; error?: never }
  | { error: ErrorDescription; title?: never };

export const notificationQueue = new ToastQueue<Notification>();

export function notifyError(error: unknown) {
  if (!isCancellation(error)) {
    notificationQueue.add({ error: describeError(error) });
  }
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
          variant={toast.content.error ? "danger" : "default"}
        >
          <Toast.Content>
            <Toast.Title>
              {toast.content.error
                ? i18n._(toast.content.error)
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
