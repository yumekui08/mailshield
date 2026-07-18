import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { getPolicyRoutes, updatePolicyRoute } from "../lib/api";
import type { PolicyDocument } from "../types";

export function usePolicyRoutes() {
  return useQuery({
    queryKey: ["policy", "routes"],
    queryFn: getPolicyRoutes,
  });
}

export function useUpdatePolicyRoute() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ dir, doc }: { dir: string; doc: PolicyDocument }) =>
      updatePolicyRoute(dir, doc),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["policy", "routes"] });
    },
  });
}
