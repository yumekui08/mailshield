import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listApprovals,
  getApproval,
  approveRequest,
  rejectRequest,
  bulkApprove,
  bulkReject,
} from "../lib/api";
import type { ApprovalFilter } from "../lib/api";

export function useApprovalList(filter: ApprovalFilter = {}) {
  return useQuery({
    queryKey: ["approvals", "list", filter],
    queryFn: () => listApprovals(filter),
    placeholderData: (prev) => prev,
  });
}

export function useBulkApprove() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ ids, comment }: { ids: string[]; comment?: string }) =>
      bulkApprove(ids, comment),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["approvals", "list"] }),
  });
}

export function useBulkReject() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ ids, comment }: { ids: string[]; comment?: string }) =>
      bulkReject(ids, comment),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["approvals", "list"] }),
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
