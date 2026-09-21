import { TanStackDevtools } from "@tanstack/react-devtools";
import { ReactQueryDevtoolsPanel } from "@tanstack/react-query-devtools";
import { TanStackRouterDevtoolsPanel } from "@tanstack/react-router-devtools";
import { application } from "@/app/application";
import { MockPanel } from "@/devtools/mock/MockPanel";

/**
 * The one mount point for the devtools shell.
 *
 * The Query and Router panels are handed the live instances because panels
 * render through a portal, where neither provider is in scope. Every import
 * here, and this element itself, are removed from the production build by
 * `@tanstack/devtools-vite`.
 */
export function Devtools() {
  return (
    <TanStackDevtools
      eventBusConfig={{ connectToServerBus: true }}
      plugins={[
        {
          name: "TanStack Query",
          render: <ReactQueryDevtoolsPanel client={application.queryClient} />,
        },
        {
          name: "TanStack Router",
          render: <TanStackRouterDevtoolsPanel router={application.router} />,
        },
        { name: "API mock", render: <MockPanel /> },
      ]}
    />
  );
}
