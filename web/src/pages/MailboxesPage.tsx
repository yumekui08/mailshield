import { useState } from "react";
import { toast } from "sonner";
import { MailPlus, Pencil, Trash2, Users, Plus, X, Inbox } from "lucide-react";
import {
  useMailboxes,
  useCreateMailbox,
  useUpdateMailbox,
  useDeleteMailbox,
  useAssignments,
  useAddAssignment,
  useRemoveAssignment,
} from "../hooks/useMailboxes";
import { useUsers } from "../hooks/useUsers";
import { usePagedList } from "../hooks/usePagedList";
import { PageHeader } from "../components/PageHeader";
import { Pagination } from "../components/Pagination";
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
import type { MailboxRecord, AssignmentRole } from "../types";

const roleBadgeVariant: Record<AssignmentRole, "blue" | "green" | "default"> = {
  member: "default",
  owner: "green",
  admin: "blue",
};

type MainDialog =
  | { type: "create" }
  | { type: "edit"; mailbox: MailboxRecord }
  | { type: "delete"; mailbox: MailboxRecord }
  | null;

function AssignmentsPanel({ mailbox, onClose }: { mailbox: MailboxRecord; onClose: () => void }) {
  const { data: assignData, isLoading: assignLoading } = useAssignments(mailbox.id);
  const { data: usersData } = useUsers();
  const addAssignment = useAddAssignment(mailbox.id);
  const removeAssignment = useRemoveAssignment(mailbox.id);

  const [addUserID, setAddUserID] = useState("");
  const [addRole, setAddRole] = useState<AssignmentRole>("member");

  const assignments = assignData?.data ?? [];
  const users = usersData?.data ?? [];

  // 既に割り当て済みの user_id+role の組み合わせを除いたユーザー候補
  const assignedKeys = new Set(assignments.map((a) => `${a.user_id}:${a.role}`));
  const availableUsers = users.filter((u) => !assignedKeys.has(`${u.id}:${addRole}`));

  function handleAdd() {
    if (!addUserID) {
      toast.error("ユーザーを選択してください");
      return;
    }
    addAssignment.mutate(
      { user_id: addUserID, role: addRole },
      {
        onSuccess: () => {
          toast.success("割り当てを追加しました");
          setAddUserID("");
        },
        onError: (err) => toast.error(`追加に失敗しました: ${err.message}`),
      }
    );
  }

  function handleRemove(userID: string, role: AssignmentRole) {
    removeAssignment.mutate(
      { user_id: userID, role },
      {
        onSuccess: () => toast.success("割り当てを削除しました"),
        onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
      }
    );
  }

  return (
    <div className="fixed inset-0 z-50 flex justify-end">
      <div className="fixed inset-0 bg-black/30" onClick={onClose} aria-hidden="true" />
      <div className="relative z-10 w-full max-w-lg bg-surface border-l border-gray-200 flex flex-col h-full overflow-y-auto">
        <div className="flex items-center justify-between px-6 py-4 border-b">
          <div>
            <div className="font-semibold text-gray-900">{mailbox.email_address}</div>
            <div className="text-xs text-gray-500 mt-0.5">ユーザー割り当て管理</div>
          </div>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 transition-colors">
            <X className="h-5 w-5" />
          </button>
        </div>

        <div className="px-6 py-4 border-b bg-gray-50">
          <div className="text-sm font-medium text-gray-700 mb-3">ユーザーを追加</div>
          <div className="flex gap-2">
            <Select
              value={addUserID}
              onChange={(e) => setAddUserID(e.target.value)}
              className="flex-1"
            >
              <option value="">ユーザーを選択…</option>
              {availableUsers.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.display_name}（{u.email}）
                </option>
              ))}
            </Select>
            <Select
              value={addRole}
              onChange={(e) => setAddRole(e.target.value as AssignmentRole)}
              className="w-40"
            >
              <option value="member">member</option>
              <option value="owner">owner</option>
              <option value="admin">admin</option>
            </Select>
            <Button onClick={handleAdd} disabled={addAssignment.isPending}>
              <Plus className="h-4 w-4" />
            </Button>
          </div>
          <div className="mt-2 text-xs text-gray-500 space-y-0.5">
            <div>member: 受信隔離の閲覧権限（to_address が一致）</div>
            <div>owner: 送信隔離の閲覧権限（from_address が一致）</div>
            <div>admin: 隔離の解放権限（policy 設定に依存）</div>
          </div>
        </div>

        <div className="flex-1 px-6 py-4">
          <div className="text-sm font-medium text-gray-700 mb-3">
            割り当て済み
            {assignments.length > 0 && (
              <Badge variant="blue" className="ml-2">{assignments.length}</Badge>
            )}
          </div>

          {assignLoading ? (
            <div className="space-y-2">
              {[...Array(3)].map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
            </div>
          ) : assignments.length === 0 ? (
            <div className="text-center text-gray-400 py-8 text-sm">
              割り当てがありません
            </div>
          ) : (
            <div className="space-y-2">
              {assignments.map((a) => (
                <div
                  key={a.id}
                  className="flex items-center justify-between rounded-md border border-gray-200 px-3 py-2 bg-surface"
                >
                  <div className="flex items-center gap-3 min-w-0">
                    <Badge variant={roleBadgeVariant[a.role]}>{a.role}</Badge>
                    <div className="min-w-0">
                      <div className="text-sm font-medium truncate">{a.user_display_name}</div>
                      <div className="text-xs text-gray-400 truncate">{a.user_email}</div>
                    </div>
                  </div>
                  <button
                    onClick={() => handleRemove(a.user_id, a.role)}
                    disabled={removeAssignment.isPending}
                    className="text-gray-400 hover:text-red-500 transition-colors shrink-0 ml-2"
                    aria-label="割り当て削除"
                  >
                    <X className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export function MailboxesPage() {
  const { data, isLoading, isError } = useMailboxes();
  const createMailbox = useCreateMailbox();
  const updateMailbox = useUpdateMailbox();
  const deleteMailbox = useDeleteMailbox();

  const [dialog, setDialog] = useState<MainDialog>(null);
  const [assignMailbox, setAssignMailbox] = useState<MailboxRecord | null>(null);

  const { pageItems: pagedMailboxes, meta, setPage } = usePagedList(data?.data, 20);

  const [createEmail, setCreateEmail] = useState("");
  const [createDisplayName, setCreateDisplayName] = useState("");
  const [editDisplayName, setEditDisplayName] = useState("");
  const [editIsActive, setEditIsActive] = useState(true);

  function openEdit(mailbox: MailboxRecord) {
    setEditDisplayName(mailbox.display_name);
    setEditIsActive(mailbox.is_active);
    setDialog({ type: "edit", mailbox });
  }

  function handleCreate() {
    if (!createEmail) {
      toast.error("メールアドレスは必須です");
      return;
    }
    createMailbox.mutate(
      { email_address: createEmail, display_name: createDisplayName || createEmail },
      {
        onSuccess: () => {
          toast.success("メールボックスを作成しました");
          setDialog(null);
          setCreateEmail("");
          setCreateDisplayName("");
        },
        onError: (err) => toast.error(`作成に失敗しました: ${err.message}`),
      }
    );
  }

  function handleUpdate() {
    if (dialog?.type !== "edit") return;
    updateMailbox.mutate(
      { id: dialog.mailbox.id, params: { display_name: editDisplayName, is_active: editIsActive } },
      {
        onSuccess: () => {
          toast.success("メールボックスを更新しました");
          setDialog(null);
        },
        onError: (err) => toast.error(`更新に失敗しました: ${err.message}`),
      }
    );
  }

  function handleDelete() {
    if (dialog?.type !== "delete") return;
    deleteMailbox.mutate(dialog.mailbox.id, {
      onSuccess: () => {
        toast.success("メールボックスを削除しました");
        setDialog(null);
      },
      onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
    });
  }

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="メールボックス管理"
        description="隔離メールの閲覧・解放権限を割り当てるメールボックス"
        count={data?.meta.total}
        actions={
          <Button onClick={() => { setCreateEmail(""); setCreateDisplayName(""); setDialog({ type: "create" }); }}>
            <MailPlus className="h-4 w-4 mr-2" />
            メールボックス追加
          </Button>
        }
      />

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          メールボックス一覧の取得に失敗しました。
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-surface overflow-hidden">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {[...Array(3)].map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead>メールアドレス</TableHead>
                <TableHead>表示名</TableHead>
                <TableHead>状態</TableHead>
                <TableHead>アクション</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {pagedMailboxes.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <Inbox className="h-8 w-8 text-gray-300" />
                      メールボックスがありません
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                pagedMailboxes.map((mailbox) => (
                  <TableRow key={mailbox.id}>
                    <TableCell className="text-sm font-medium">{mailbox.email_address}</TableCell>
                    <TableCell className="text-sm text-gray-600">{mailbox.display_name}</TableCell>
                    <TableCell>
                      <Badge variant={mailbox.is_active ? "green" : "default"}>
                        {mailbox.is_active ? "有効" : "無効"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setAssignMailbox(mailbox)}
                        >
                          <Users className="h-3.5 w-3.5 mr-1" />
                          割り当て
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => openEdit(mailbox)}
                        >
                          <Pencil className="h-3.5 w-3.5 mr-1" />
                          編集
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => setDialog({ type: "delete", mailbox })}
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
        {data && (
          <Pagination
            meta={meta}
            onPageChange={setPage}
            className="border-t border-gray-200 bg-gray-50 px-3 py-2"
          />
        )}
      </div>

      {/* 作成ダイアログ */}
      <Dialog open={dialog?.type === "create"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>メールボックスを追加</DialogTitle>
          <DialogDescription>新しい内部メールアドレスを登録します。</DialogDescription>
        </DialogHeader>
        <div className="px-5 py-4 space-y-3">
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">メールアドレス *</label>
            <Input
              type="email"
              placeholder="user@internal.test"
              value={createEmail}
              onChange={(e) => setCreateEmail(e.target.value)}
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
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button onClick={handleCreate} disabled={createMailbox.isPending}>
            {createMailbox.isPending ? "作成中…" : "作成する"}
          </Button>
        </DialogFooter>
      </Dialog>

      {/* 編集ダイアログ */}
      <Dialog open={dialog?.type === "edit"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>メールボックスを編集</DialogTitle>
          <DialogDescription>
            {dialog?.type === "edit" && dialog.mailbox.email_address}
          </DialogDescription>
        </DialogHeader>
        <div className="px-5 py-4 space-y-3">
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">表示名</label>
            <Input value={editDisplayName} onChange={(e) => setEditDisplayName(e.target.value)} />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">状態</label>
            <Select value={String(editIsActive)} onChange={(e) => setEditIsActive(e.target.value === "true")}>
              <option value="true">有効</option>
              <option value="false">無効</option>
            </Select>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button onClick={handleUpdate} disabled={updateMailbox.isPending}>
            {updateMailbox.isPending ? "更新中…" : "更新する"}
          </Button>
        </DialogFooter>
      </Dialog>

      {/* 削除確認ダイアログ */}
      <Dialog open={dialog?.type === "delete"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>メールボックスを削除しますか？</DialogTitle>
          <DialogDescription>
            {dialog?.type === "delete" && (
              <>「{dialog.mailbox.email_address}」を削除します。割り当て情報もすべて削除されます。</>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button variant="destructive" onClick={handleDelete} disabled={deleteMailbox.isPending}>
            {deleteMailbox.isPending ? "削除中…" : "削除する"}
          </Button>
        </DialogFooter>
      </Dialog>

      {/* 割り当てパネル（スライドドロワー） */}
      {assignMailbox && (
        <AssignmentsPanel
          mailbox={assignMailbox}
          onClose={() => setAssignMailbox(null)}
        />
      )}
    </div>
  );
}
