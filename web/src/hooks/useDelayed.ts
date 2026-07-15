import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listDelayed, cancelDelayed, sendDelayedNow } from "../lib/api";

export function useDelayedList() {
  return useQuery({
    queryKey: ["delayed", "list"],
    queryFn: listDelayed,
    refetchInterval: 30_000,
  });
}

export function useCancelDelayed() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => cancelDelayed(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["delayed", "list"] });
    },
  });
}

export function useSendDelayedNow() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => sendDelayedNow(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["delayed", "list"] });
    },
  });
}
