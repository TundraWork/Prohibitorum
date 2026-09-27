import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { AppEnvironment } from "@/app/AppEnvironment";
import { application } from "@/app/application";

export function App() {
  return (
    <QueryClientProvider client={application.queryClient}>
      <AppEnvironment>
        <RouterProvider router={application.router} />
      </AppEnvironment>
    </QueryClientProvider>
  );
}
