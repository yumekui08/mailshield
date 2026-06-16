import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  listMailboxes,
  createMailbox,
  updateMailbox,
  deleteMailbox,
  listAssignments,
  addAssignment,
  removeAssignment,
} from "../lib/api";
import type { AssignmentRole } from "../types";

export function useMailboxes() {
  return useQuery({ queryKey: ["mailboxes"], queryFn: listMailboxes });
}

export function useCreateMailbox() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: createMailbox,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mailboxes"] }),
  });
}

export function useUpdateMailbox() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, params }: { id: string; params: { display_name?: string; is_active?: boolean } }) =>
      updateMailbox(id, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mailboxes"] }),
  });
}

export function useDeleteMailbox() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteMailbox(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["mailboxes"] }),
  });
}

export function useAssignments(mailboxId: string) {
  return useQuery({
    queryKey: ["assignments", mailboxId],
    queryFn: () => listAssignments(mailboxId),
    enabled: !!mailboxId,
  });
}

export function useAddAssignment(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: { user_id: string; role: AssignmentRole }) =>
      addAssignment(mailboxId, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["assignments", mailboxId] }),
  });
}

export function useRemoveAssignment(mailboxId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (params: { user_id: string; role: AssignmentRole }) =>
      removeAssignment(mailboxId, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["assignments", mailboxId] }),
  });
}
