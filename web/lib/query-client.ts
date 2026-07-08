import { QueryClient } from "@tanstack/react-query";

/** The app's single QueryClient, shared by every query and mutation hook. */
export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 2,
      refetchOnWindowFocus: false,
      staleTime: 30 * 1000,
      gcTime: 5 * 60 * 1000,
    },
  },
});
