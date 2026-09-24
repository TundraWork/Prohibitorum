import { createQueryClient } from "@/app/query-client";
import { createAppRouter } from "@/app/router";
import {
  notifyError,
  notifySuccess,
} from "@/components/custom/AppNotifications";

const queryClient = createQueryClient(notifyError, notifySuccess);

/**
 * The console's long-lived query client and router.
 *
 * They live outside `App` so the devtools host — mounted beside `App` rather
 * than inside it — can be handed the very instances the pages render against.
 */
export const application = {
  queryClient,
  router: createAppRouter({ queryClient }),
};
