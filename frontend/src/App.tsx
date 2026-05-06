import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";

import { createQueryClient } from "@/lib/query";
import { createAppRouter } from "@/routes/router";

const queryClient = createQueryClient();
const router = createAppRouter();

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
