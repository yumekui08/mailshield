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
  listRoutings,
  createRouting,
  updateRouting,
  deleteRouting,
  listPolicyInstances,
  createPolicyInstance,
  updatePolicyInstance,
  deletePolicyInstance,
} from "../lib/api";
import type { WorkerInstance, ConfigVariable, Routing, PolicyInstance } from "../types";

type RoutingInput = Omit<Routing, "id" | "is_catchall" | "created_at" | "updated_at">;
type PolicyInput = { alias: string; display_name: string; content: string };

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

// ─── ルーティング ──
export function useRoutings() {
  return useQuery({ queryKey: ["routings"], queryFn: listRoutings });
}

export function useCreateRouting() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: RoutingInput) => createRouting(params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["routings"] }),
  });
}

export function useUpdateRouting() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, params }: { id: string; params: RoutingInput }) => updateRouting(id, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["routings"] }),
  });
}

export function useDeleteRouting() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteRouting(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["routings"] }),
  });
}

// ─── ポリシーインスタンス ──
export function usePolicyInstances() {
  return useQuery({ queryKey: ["policy-instances"], queryFn: listPolicyInstances });
}
export function useCreatePolicyInstance() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: PolicyInput) => createPolicyInstance(params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["policy-instances"] }),
  });
}
export function useUpdatePolicyInstance() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, params }: { id: string; params: PolicyInput }) => updatePolicyInstance(id, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["policy-instances"] }),
  });
}
export function useDeletePolicyInstance() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deletePolicyInstance(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["policy-instances"] }),
  });
}

export type { WorkerInstance, ConfigVariable, Routing, PolicyInstance };
