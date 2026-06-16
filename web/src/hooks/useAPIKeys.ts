import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { createAPIKey, listAPIKeys, revokeAPIKey } from "../lib/api";
import type { CreateAPIKeyRequest } from "../types";

export function useAPIKeys() {
  return useQuery({
    queryKey: ["api-keys"],
    queryFn: () => listAPIKeys(),
  });
}

export function useCreateAPIKey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateAPIKeyRequest) => createAPIKey(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
}

export function useRevokeAPIKey() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => revokeAPIKey(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });
}
