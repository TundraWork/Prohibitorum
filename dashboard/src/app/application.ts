import { createQueryClient } from "@/app/query-client";
import { createAppRouter } from "@/app/router";
import {
  notifyError,
  notifySuccess,
} from "@/components/custom/AppNotifications";

// The router is created after the query client, so the maintenance handler
// reads it only when a request finds maintenance on.
const queryClient = createQueryClient(notifyError, notifySuccess, () => {
  void application.router.navigate({ to: "/maintenance" });
});

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
