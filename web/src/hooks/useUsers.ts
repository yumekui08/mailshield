import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { listUsers, createUser, updateUser, deleteUser } from "../lib/api";

export function useUsers() {
  return useQuery({
    queryKey: ["users"],
    queryFn: listUsers,
  });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: createUser,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, params }: { id: string; params: { role?: string; password?: string } }) =>
      updateUser(id, params),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => deleteUser(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}
