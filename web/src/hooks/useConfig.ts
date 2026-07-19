import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listWorkerInstances,
  createWorkerInstance,
  updateWorkerInstance,
  deleteWorkerInstance,
  listConfigVariables,
  createConfigVariable,
  updateConfigVariable,
  deleteConfigVariable,
} from "../lib/api";
import type { WorkerInstance, ConfigVariable } from "../types";

type WorkerInstanceInput = Omit<WorkerInstance, "id" | "created_at" | "updated_at">;
type VariableInput = { key: string; value: string; description: string };

// ─── ワーカーインスタンス ──
export function useWorkerInstances() {
  return useQuery({ queryKey: ["worker-instances"], queryFn: listWorkerInstances });
}

export function useCreateWorkerInstance() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: WorkerInstanceInput) => createWorkerInstance(params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["worker-instances"] }),
  });
}

export function useUpdateWorkerInstance() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, params }: { id: string; params: WorkerInstanceInput }) =>
      updateWorkerInstance(id, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["worker-instances"] }),
  });
}

export function useDeleteWorkerInstance() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteWorkerInstance(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["worker-instances"] }),
  });
}

// ─── 設定変数 ──
export function useConfigVariables() {
  return useQuery({ queryKey: ["config-variables"], queryFn: listConfigVariables });
}

export function useCreateConfigVariable() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: VariableInput) => createConfigVariable(params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["config-variables"] }),
  });
}

export function useUpdateConfigVariable() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, params }: { id: string; params: VariableInput }) =>
      updateConfigVariable(id, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["config-variables"] }),
  });
}

export function useDeleteConfigVariable() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteConfigVariable(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["config-variables"] }),
  });
}

export type { WorkerInstance, ConfigVariable };
