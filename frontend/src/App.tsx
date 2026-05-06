import { QueryClientProvider } from "@tanstack/react-query";
import { RouterProvider } from "react-router-dom";

import "@/i18n/i18n";
import { LocaleSync } from "@/features/preferences/LocaleSync";
import { ThemeModeSync } from "@/features/preferences/ThemeModeSync";
import { createQueryClient } from "@/lib/query";
import { createAppRouter } from "@/routes/router";

const queryClient = createQueryClient();
const router = createAppRouter();

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <LocaleSync />
      <ThemeModeSync />
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}
