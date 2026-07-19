import { useState } from "react";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, Variable } from "lucide-react";
import {
  useConfigVariables,
  useCreateConfigVariable,
  useUpdateConfigVariable,
  useDeleteConfigVariable,
} from "../hooks/useConfig";
import { PageHeader } from "../components/PageHeader";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
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
import type { ConfigVariable } from "../types";

type DialogState =
  | { type: "create" }
  | { type: "edit"; variable: ConfigVariable }
  | { type: "delete"; variable: ConfigVariable }
  | null;

export function VariablesPage() {
  const { data, isLoading, isError } = useConfigVariables();
  const createVar = useCreateConfigVariable();
  const updateVar = useUpdateConfigVariable();
  const deleteVar = useDeleteConfigVariable();

  const [dialog, setDialog] = useState<DialogState>(null);
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [description, setDescription] = useState("");

  const variables = data?.data ?? [];

  function openCreate() {
    setKey("");
    setValue("");
    setDescription("");
    setDialog({ type: "create" });
  }
  function openEdit(v: ConfigVariable) {
    setKey(v.key);
    setValue(v.value);
    setDescription(v.description);
    setDialog({ type: "edit", variable: v });
  }

  function handleSave() {
    const params = { key: key.trim(), value, description: description.trim() };
    if (!params.key) {
      toast.error("キーは必須です");
      return;
    }
    const opts = {
      onSuccess: () => {
        toast.success("保存しました");
        setDialog(null);
      },
      onError: (err: Error) => toast.error(`保存に失敗しました: ${err.message}`),
    };
    if (dialog?.type === "edit") {
      updateVar.mutate({ id: dialog.variable.id, params }, opts);
    } else {
      createVar.mutate(params, opts);
    }
  }

  function handleDelete() {
    if (dialog?.type !== "delete") return;
    deleteVar.mutate(dialog.variable.id, {
      onSuccess: () => {
        toast.success("削除しました");
        setDialog(null);
      },
      onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
    });
  }

  const isSaving = createVar.isPending || updateVar.isPending;

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="設定変数"
        description="ルーティング・ポリシー・ワーカー設定から ${VAR} で参照できる共有値（非機密）"
        count={data ? variables.length : undefined}
        actions={
          <Button onClick={openCreate}>
            <Plus className="h-4 w-4 mr-2" />
            変数を追加
          </Button>
        }
      />

      <div className="rounded border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
        パスワードなどのシークレットはここに入れず、OS 環境変数のままにしてください（変数はエクスポート対象・平文表示のため）。
      </div>

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          設定変数の取得に失敗しました。
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
                <TableHead>キー</TableHead>
                <TableHead>値</TableHead>
                <TableHead>説明</TableHead>
                <TableHead>アクション</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {variables.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <Variable className="h-8 w-8 text-gray-300" />
                      変数がありません。「変数を追加」から作成してください。
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                variables.map((v) => (
                  <TableRow key={v.id}>
                    <TableCell className="font-mono text-sm">${"{"}{v.key}{"}"}</TableCell>
                    <TableCell className="text-sm text-gray-700 max-w-64 truncate">{v.value}</TableCell>
                    <TableCell className="text-sm text-gray-500 max-w-64 truncate">{v.description}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button variant="outline" size="sm" onClick={() => openEdit(v)}>
                          <Pencil className="h-3.5 w-3.5 mr-1" />編集
                        </Button>
                        <Button variant="destructive" size="sm" onClick={() => setDialog({ type: "delete", variable: v })}>
                          <Trash2 className="h-3.5 w-3.5 mr-1" />削除
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

      {/* 作成・編集ダイアログ */}
      <Dialog open={dialog?.type === "create" || dialog?.type === "edit"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>{dialog?.type === "edit" ? "変数を編集" : "変数を追加"}</DialogTitle>
          <DialogDescription>設定内から <code>${"{"}KEY{"}"}</code> の形で参照できます。</DialogDescription>
        </DialogHeader>
        <div className="px-5 py-4 space-y-3">
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">キー *</label>
            <Input
              placeholder="INTERNAL_DOMAIN"
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
            <p className="text-xs text-gray-400">英字・_ で始まり、英数字・_ のみ。</p>
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">値</label>
            <Input placeholder="@example.com" value={value} onChange={(e) => setValue(e.target.value)} />
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">説明</label>
            <Input
              placeholder="受信/送信判定に使う自組織ドメイン"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button onClick={handleSave} disabled={isSaving}>{isSaving ? "保存中…" : "保存する"}</Button>
        </DialogFooter>
      </Dialog>

      {/* 削除確認 */}
      <Dialog open={dialog?.type === "delete"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>変数を削除しますか？</DialogTitle>
          <DialogDescription>
            {dialog?.type === "delete" && (
              <>「{dialog.variable.key}」を削除します。この変数を参照している設定があると、次回の設定検証でエラーになります。</>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button variant="destructive" onClick={handleDelete} disabled={deleteVar.isPending}>
            {deleteVar.isPending ? "削除中…" : "削除する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
