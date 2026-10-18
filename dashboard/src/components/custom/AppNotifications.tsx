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
import { NotificationToasts } from "@/components/custom/NotificationToasts";
import { RequestDetailsDialog } from "@/components/custom/RequestDetailsDialog";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { toastManager } from "@/components/ui/toast";

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

/**
 * How long a success stays up. An error never times out: on a public page it
 * is the only report of a failed action, so it stays until dismissed. Either
 * one holds while the pointer or focus is on it.
 */
const successTimeout = 5000;

const shown = new Set<string>();
/** Opens the request details; set while the notifications are mounted. */
let openDetails: ((exchange: RequestExchange) => void) | undefined;

/** A descriptor drawn at render, so a toast on screen follows the language. */
function Message({ descriptor }: { descriptor: MessageDescriptor }) {
  const { i18n } = useLingui();
  return <>{i18n._(descriptor)}</>;
}

function describe(content: Notification, timeout?: number) {
  if (content.error) {
    const { error, exchange } = content;
    return {
      type: "error",
      title: <Message descriptor={error} />,
      description: error.requestId ? (
        <Trans id="notification.request_id">
          Request ID: {error.requestId}
        </Trans>
      ) : undefined,
      actionProps: exchange
        ? {
            children: <Trans id="notification.details">Details</Trans>,
            onClick: () => openDetails?.(exchange),
          }
        : undefined,
      timeout: 0,
    };
  }
  if (content.success) {
    return {
      type: "success",
      title: <Message descriptor={content.success} />,
      timeout,
    };
  }
  return { title: content.title, timeout };
}

/** The toast queue the rest of the console adds to, over coss's toast manager. */
export const notificationQueue = {
  add(content: Notification, options?: { timeout?: number }) {
    const id: string = toastManager.add({
      ...describe(content, options?.timeout),
      // Forget the id once the toast closes, so the set does not grow forever.
      onClose: () => shown.delete(id),
      onRemove: () => shown.delete(id),
    });
    shown.add(id);
    return id;
  },
  clear() {
    for (const id of shown) toastManager.close(id);
    shown.clear();
  },
};

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
  const [details, setDetails] = useState<RequestExchange | null>(null);
  useEffect(() => {
    openDetails = setDetails;
    return () => {
      openDetails = undefined;
      notificationQueue.clear();
    };
  }, []);

  return (
    <>
      <NotificationToasts container={getTopLayerContainer()} />
      <RequestDetailsDialog
        exchange={details}
        onClose={() => setDetails(null)}
      />
    </>
  );
}
