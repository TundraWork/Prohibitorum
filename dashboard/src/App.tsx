import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { useState } from "react";
import { createQueryClient } from "@/app/query-client";
import { createAppRouter } from "@/app/router";
import { notifyError } from "@/components/custom/AppNotifications";

export function App() {
  const [application] = useState(() => {
    const queryClient = createQueryClient(notifyError);
    return { queryClient, router: createAppRouter({ queryClient }) };
  });
  return (
    <QueryClientProvider client={application.queryClient}>
      <RouterProvider router={application.router} />
    </QueryClientProvider>
  );
}
