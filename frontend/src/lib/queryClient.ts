import { ApiError } from "@/api/client";
import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 0,
      gcTime: 5 * 60 * 1000,
      /**
       * A 4xx is an answer: the request was wrong, or it was refused, and
       * asking again will not change either. Anything else is a transport
       * failure — a dropped connection, a proxy 502 while the server restarts —
       * which on an always-on dashboard used to leave the page in a hard error
       * state until someone pressed Retry. Two attempts ride over a restart;
       * beyond that the error is worth showing.
       */
      retry: (failureCount, error) => {
        if (error instanceof ApiError && error.status >= 400 && error.status < 500) {
          return false;
        }

        return failureCount < 2;
      },
      refetchOnWindowFocus: false,
    },
  },
});
