import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listApprovals,
  getApproval,
  approveRequest,
  rejectRequest,
} from "../lib/api";

export function useApprovalList() {
  return useQuery({
    queryKey: ["approvals", "list"],
    queryFn: listApprovals,
  });
}

export function useApprovalDetail(id: string) {
  return useQuery({
    queryKey: ["approvals", "detail", id],
    queryFn: () => getApproval(id),
    enabled: !!id,
  });
}

export function useApprove() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, comment }: { id: string; comment?: string }) =>
      approveRequest(id, comment),
    onSuccess: (_data, { id }) => {
      queryClient.removeQueries({ queryKey: ["approvals", "detail", id] });
      queryClient.invalidateQueries({ queryKey: ["approvals", "list"] });
    },
  });
}

export function useReject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, comment }: { id: string; comment?: string }) =>
      rejectRequest(id, comment),
    onSuccess: (_data, { id }) => {
      queryClient.removeQueries({ queryKey: ["approvals", "detail", id] });
      queryClient.invalidateQueries({ queryKey: ["approvals", "list"] });
    },
  });
}
