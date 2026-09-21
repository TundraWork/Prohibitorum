import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "@tanstack/react-router";
import { application } from "@/app/application";

export function App() {
  return (
    <QueryClientProvider client={application.queryClient}>
      <RouterProvider router={application.router} />
    </QueryClientProvider>
  );
}
