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
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { ToastProvider, toastManager } from "@/components/ui/toast";

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
 * How long a success stays up. An error closes after the library's default.
 * Either one holds while the pointer or focus is on it, which is the time to
 * open an error's details.
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
      timeout,
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
    const id = toastManager.add(describe(content, options?.timeout));
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
      <ToastProvider
        position="bottom-right"
        portalProps={{ container: getTopLayerContainer() }}
      />
      <RequestDetailsDialog
        exchange={details}
        onClose={() => setDetails(null)}
      />
    </>
  );
}
