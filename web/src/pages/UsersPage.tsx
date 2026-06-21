import { useState } from "react";
import { toast } from "sonner";
import { UserPlus, Pencil, Trash2, Users, UserCheck } from "lucide-react";
import { useUsers, useCreateUser, useUpdateUser, useDeleteUser } from "../hooks/useUsers";
import { useSetUserApprover } from "../hooks/useApprovals";
import { useMe } from "../hooks/useAuth";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Select } from "../components/ui/select";
import { Badge } from "../components/ui/badge";
import { Skeleton } from "../components/ui/skeleton";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";
import {
  Dialog,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "../components/ui/dialog";
import type { UserRecord, Role } from "../types";

const roleLabel: Record<Role, string> = {
  admin: "管理者",
  operator: "オペレーター",
  viewer: "閲覧者",
};

const roleBadgeVariant: Record<Role, "blue" | "green" | "default"> = {
  admin: "blue",
  operator: "green",
  viewer: "default",
};

type DialogState =
  | { type: "create" }
  | { type: "edit"; user: UserRecord }
  | { type: "delete"; user: UserRecord }
  | { type: "approver"; user: UserRecord }
  | null;

export function UsersPage() {
  const { data: me } = useMe();
  const { data, isLoading, isError } = useUsers();
  const createUser = useCreateUser();
  const updateUser = useUpdateUser();
  const deleteUser = useDeleteUser();
  const setApprover = useSetUserApprover();

  const [dialog, setDialog] = useState<DialogState>(null);
  const [approverSelectId, setApproverSelectId] = useState<string>("");

  // フォームステート（作成）
  const [createEmail, setCreateEmail] = useState("");
  const [createDisplayName, setCreateDisplayName] = useState("");
  const [createPassword, setCreatePassword] = useState("");
  const [createRole, setCreateRole] = useState<Role>("viewer");

  // フォームステート（編集）
  const [editRole, setEditRole] = useState<Role>("viewer");
  const [editPassword, setEditPassword] = useState("");
  const [editDisplayName, setEditDisplayName] = useState("");

  // admin 以外はアクセス不可
  if (me && me.role !== "admin") {
    return (
      <div className="p-6">
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          この画面は管理者（admin）のみアクセスできます。
        </div>
      </div>
    );
  }

  function openCreate() {
    setCreateEmail("");
    setCreateDisplayName("");
    setCreatePassword("");
    setCreateRole("viewer");
    setDialog({ type: "create" });
  }

  function openEdit(user: UserRecord) {
    setEditRole(user.role);
    setEditPassword("");
    setEditDisplayName(user.display_name);
    setDialog({ type: "edit", user });
  }

  function handleCreate() {
    if (!createEmail || !createPassword) {
      toast.error("メールアドレスとパスワードは必須です");
      return;
    }
    if (createPassword.length < 8) {
      toast.error("パスワードは8文字以上にしてください");
      return;
    }
    createUser.mutate(
      {
        email: createEmail,
        password: createPassword,
        display_name: createDisplayName || createEmail,
        role: createRole,
      },
      {
        onSuccess: () => {
          toast.success("ユーザーを作成しました");
          setDialog(null);
        },
        onError: (err) => toast.error(`作成に失敗しました: ${err.message}`),
      }
    );
  }

  function handleUpdate() {
    if (dialog?.type !== "edit") return;
    const params: { role?: string; password?: string; display_name?: string } = {};
    if (editRole !== dialog.user.role) params.role = editRole;
    if (editPassword) {
      if (editPassword.length < 8) {
        toast.error("パスワードは8文字以上にしてください");
        return;
      }
      params.password = editPassword;
    }
    if (editDisplayName !== dialog.user.display_name) params.display_name = editDisplayName;

    if (Object.keys(params).length === 0) {
      setDialog(null);
      return;
    }

    updateUser.mutate(
      { id: dialog.user.id, params },
      {
        onSuccess: () => {
          toast.success("ユーザーを更新しました");
          setDialog(null);
        },
        onError: (err) => toast.error(`更新に失敗しました: ${err.message}`),
      }
    );
  }

  function handleDelete() {
    if (dialog?.type !== "delete") return;
    deleteUser.mutate(dialog.user.id, {
      onSuccess: () => {
        toast.success("ユーザーを削除しました");
        setDialog(null);
      },
      onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
    });
  }

  function openApprover(user: UserRecord) {
    setApproverSelectId(user.approver_id ?? "");
    setDialog({ type: "approver", user });
  }

  function handleSetApprover() {
    if (dialog?.type !== "approver") return;
    const approverId = approverSelectId || null;
    setApprover.mutate(
      { userId: dialog.user.id, approverId },
      {
        onSuccess: () => {
          toast.success("承認者を設定しました");
          setDialog(null);
        },
        onError: (err) => toast.error(`設定に失敗しました: ${err.message}`),
      }
    );
  }

  const users = data?.data ?? [];

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold text-gray-900">ユーザー管理</h1>
          {data && (
            <Badge variant="blue">{data.meta.total} 人</Badge>
          )}
        </div>
        <Button onClick={openCreate}>
          <UserPlus className="h-4 w-4 mr-2" />
          ユーザー追加
        </Button>
      </div>

      {isError && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          ユーザー一覧の取得に失敗しました。
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-white overflow-hidden">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>メールアドレス</TableHead>
                <TableHead>表示名</TableHead>
                <TableHead>ロール</TableHead>
                <TableHead>状態</TableHead>
                <TableHead>承認者</TableHead>
                <TableHead>アクション</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <Users className="h-8 w-8 text-gray-300" />
                      ユーザーがいません
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                users.map((user) => (
                  <TableRow key={user.id}>
                    <TableCell className="text-sm font-medium">{user.email}</TableCell>
                    <TableCell className="text-sm text-gray-600">{user.display_name}</TableCell>
                    <TableCell>
                      <Badge variant={roleBadgeVariant[user.role]}>
                        {roleLabel[user.role]}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant={user.is_active ? "green" : "default"}>
                        {user.is_active ? "有効" : "無効"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-xs text-gray-500">
                      {user.approver_id
                        ? users.find((u) => u.id === user.approver_id)?.email ?? user.approver_id.slice(0, 8) + "…"
                        : "—"}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => openEdit(user)}
                          disabled={updateUser.isPending}
                        >
                          <Pencil className="h-3.5 w-3.5 mr-1" />
                          編集
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => openApprover(user)}
                        >
                          <UserCheck className="h-3.5 w-3.5 mr-1" />
                          承認者
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => setDialog({ type: "delete", user })}
                          disabled={deleteUser.isPending || user.id === me?.sub}
                          title={user.id === me?.sub ? "自分自身は削除できません" : undefined}
                        >
                          <Trash2 className="h-3.5 w-3.5 mr-1" />
                          削除
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </div>

      {/* ユーザー作成ダイアログ */}
      <Dialog open={dialog?.type === "create"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>ユーザーを追加</DialogTitle>
          <DialogDescription>新しいユーザーを作成します。</DialogDescription>
        </DialogHeader>
        <div className="px-6 pb-4 space-y-3">
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">メールアドレス *</label>
            <Input
              type="email"
              placeholder="user@example.com"
              value={createEmail}
              onChange={(e) => setCreateEmail(e.target.value)}
              autoComplete="off"
            />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">表示名</label>
            <Input
              placeholder="（省略時はメールアドレス）"
              value={createDisplayName}
              onChange={(e) => setCreateDisplayName(e.target.value)}
            />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">パスワード *（8文字以上）</label>
            <Input
              type="password"
              placeholder="パスワード"
              value={createPassword}
              onChange={(e) => setCreatePassword(e.target.value)}
              autoComplete="new-password"
            />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">ロール</label>
            <Select value={createRole} onChange={(e) => setCreateRole(e.target.value as Role)}>
              <option value="viewer">閲覧者 (viewer)</option>
              <option value="operator">オペレーター (operator)</option>
              <option value="admin">管理者 (admin)</option>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>
            キャンセル
          </Button>
          <Button onClick={handleCreate} disabled={createUser.isPending}>
            {createUser.isPending ? "作成中..." : "作成する"}
          </Button>
        </DialogFooter>
      </Dialog>

      {/* ユーザー編集ダイアログ */}
      <Dialog open={dialog?.type === "edit"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>ユーザーを編集</DialogTitle>
          <DialogDescription>
            {dialog?.type === "edit" && dialog.user.email}
          </DialogDescription>
        </DialogHeader>
        <div className="px-6 pb-4 space-y-3">
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">表示名</label>
            <Input
              placeholder="表示名"
              value={editDisplayName}
              onChange={(e) => setEditDisplayName(e.target.value)}
            />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">ロール</label>
            <Select value={editRole} onChange={(e) => setEditRole(e.target.value as Role)}>
              <option value="viewer">閲覧者 (viewer)</option>
              <option value="operator">オペレーター (operator)</option>
              <option value="admin">管理者 (admin)</option>
            </Select>
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">
              新しいパスワード（変更しない場合は空欄）
            </label>
            <Input
              type="password"
              placeholder="8文字以上"
              value={editPassword}
              onChange={(e) => setEditPassword(e.target.value)}
              autoComplete="new-password"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>
            キャンセル
          </Button>
          <Button onClick={handleUpdate} disabled={updateUser.isPending}>
            {updateUser.isPending ? "更新中..." : "更新する"}
          </Button>
        </DialogFooter>
      </Dialog>

      {/* 削除確認ダイアログ */}
      <Dialog open={dialog?.type === "delete"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>ユーザーを削除しますか？</DialogTitle>
          <DialogDescription>
            {dialog?.type === "delete" && (
              <>「{dialog.user.email}」を削除します。この操作は取り消せません。</>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>
            キャンセル
          </Button>
          <Button
            variant="destructive"
            onClick={handleDelete}
            disabled={deleteUser.isPending}
          >
            {deleteUser.isPending ? "削除中..." : "削除する"}
          </Button>
        </DialogFooter>
      </Dialog>

      {/* 承認者設定ダイアログ */}
      <Dialog open={dialog?.type === "approver"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>承認者を設定</DialogTitle>
          <DialogDescription>
            {dialog?.type === "approver" && (
              <>「{dialog.user.email}」のメール送信を承認するユーザーを選択します。</>
            )}
          </DialogDescription>
        </DialogHeader>
        <div className="px-6 pb-4 space-y-1">
          <label className="text-sm font-medium text-gray-700">承認者</label>
          <Select
            value={approverSelectId}
            onChange={(e) => setApproverSelectId(e.target.value)}
          >
            <option value="">（設定なし）</option>
            {users
              .filter((u) => dialog?.type === "approver" && u.id !== dialog.user.id)
              .map((u) => (
                <option key={u.id} value={u.id}>
                  {u.display_name} ({u.email})
                </option>
              ))}
          </Select>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>
            キャンセル
          </Button>
          <Button onClick={handleSetApprover} disabled={setApprover.isPending}>
            {setApprover.isPending ? "設定中..." : "保存する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
