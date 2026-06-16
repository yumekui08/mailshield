import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getMe,
  getProviders,
  loginStandalone,
  logout,
  setup,
  ApiError,
} from "../lib/api";

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: getMe,
    retry: (failureCount, error) => {
      if (error instanceof ApiError && error.status === 401) return false;
      return failureCount < 2;
    },
  });
}

export function useProviders() {
  return useQuery({
    queryKey: ["auth-providers"],
    queryFn: getProviders,
    staleTime: Infinity,
    retry: false,
  });
}

export function useLoginStandalone() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ email, password }: { email: string; password: string }) =>
      loginStandalone(email, password),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["me"] });
    },
  });
}

export function useLogout() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: logout,
    onSuccess: () => {
      queryClient.clear();
      window.location.href = "/login";
    },
  });
}

export function useSetup() {
  return useMutation({
    mutationFn: ({
      email,
      password,
      displayName,
    }: {
      email: string;
      password: string;
      displayName?: string;
    }) => setup(email, password, displayName),
  });
}
