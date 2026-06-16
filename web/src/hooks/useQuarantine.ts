import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  getQuarantineList,
  getQuarantineDetail,
  releaseQuarantine,
  deleteQuarantine,
  bulkReleaseQuarantine,
  bulkDeleteQuarantine,
  type QuarantineListParams,
} from "../lib/api";

export function useQuarantineList(params: QuarantineListParams) {
  return useQuery({
    queryKey: ["quarantine", "list", params],
    queryFn: () => getQuarantineList(params),
  });
}

export function useQuarantineDetail(id: string) {
  return useQuery({
    queryKey: ["quarantine", "detail", id],
    queryFn: () => getQuarantineDetail(id),
  });
}

export function useRelease() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => releaseQuarantine(id),
    onSuccess: (_data, id) => {
      // 解放後は status=delivered になり GET /quarantine/{id} が 404 を返す。
      // invalidateQueries だと詳細が自動再フェッチされてエラー表示が瞬間的に出るため、
      // removeQueries でキャッシュを削除して再フェッチを防ぐ。
      queryClient.removeQueries({ queryKey: ["quarantine", "detail", id] });
      queryClient.invalidateQueries({ queryKey: ["quarantine", "list"] });
    },
  });
}

export function useDelete() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteQuarantine(id),
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: ["quarantine", "detail", id] });
      queryClient.invalidateQueries({ queryKey: ["quarantine", "list"] });
    },
  });
}

export function useBulkRelease() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (ids: string[]) => bulkReleaseQuarantine(ids),
    onSuccess: (_data, ids) => {
      ids.forEach((id) => queryClient.removeQueries({ queryKey: ["quarantine", "detail", id] }));
      queryClient.invalidateQueries({ queryKey: ["quarantine", "list"] });
    },
  });
}

export function useBulkDelete() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (ids: string[]) => bulkDeleteQuarantine(ids),
    onSuccess: (_data, ids) => {
      ids.forEach((id) => queryClient.removeQueries({ queryKey: ["quarantine", "detail", id] }));
      queryClient.invalidateQueries({ queryKey: ["quarantine", "list"] });
    },
  });
}
