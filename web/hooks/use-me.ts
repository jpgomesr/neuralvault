import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getMe, login, logout } from "@/lib/api/auth";

export const meQueryKey = ["me"] as const;

/**
 * useMe caches the authenticated user. `me` mirrors the app's existing
 * tri-state convention: undefined while loading, null when unauthenticated
 * (or on a failed fetch, after retries), and the user once signed in.
 */
export function useMe() {
  const query = useQuery({ queryKey: meQueryKey, queryFn: getMe });
  const me = query.isPending ? undefined : (query.data ?? null);
  return { ...query, me };
}

/** useLoginMutation signs in and invalidates the cached user on success. */
export function useLoginMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) =>
      login(email, password),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: meQueryKey });
    },
  });
}

/** useLogoutMutation signs out and invalidates the user and workspace caches. */
export function useLogoutMutation() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: logout,
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: meQueryKey });
      void queryClient.invalidateQueries({ queryKey: ["workspaces"] });
    },
  });
}
