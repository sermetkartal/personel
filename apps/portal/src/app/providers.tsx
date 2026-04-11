"use client";

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState } from "react";
import { Toaster } from "sonner";

export function Providers({
  children,
}: {
  children: React.ReactNode;
}): JSX.Element {
  const [queryClient] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: {
            staleTime: 60 * 1000,       // 1 minute
            refetchOnWindowFocus: false, // Avoid noisy refetches on corporate laptops
            retry: 1,
          },
        },
      })
  );

  return (
    <QueryClientProvider client={queryClient}>
      {children}
      <Toaster
        position="bottom-right"
        toastOptions={{
          classNames: {
            toast:
              "font-sans text-sm rounded-xl border border-warm-200 shadow-card",
            title: "font-medium text-warm-900",
            description: "text-warm-600",
          },
        }}
      />
    </QueryClientProvider>
  );
}
