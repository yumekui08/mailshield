import { useState } from "react";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, ScrollText } from "lucide-react";
import {
  usePolicyInstances,
  useCreatePolicyInstance,
  useUpdatePolicyInstance,
  useDeletePolicyInstance,
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
import type { PolicyInstance } from "../types";

const SAMPLE_CONTENT = `rules:
  - name: virus_block
    condition: "av_internal.detected == true"
    action: reject
  - name: default_deliver
    condition: "true"
    action: deliver
`;

type DialogState =
  | { type: "create" }
  | { type: "edit"; policy: PolicyInstance }
  | { type: "delete"; policy: PolicyInstance }
  | null;

export function PolicyInstancesPage() {
  const { data, isLoading, isError } = usePolicyInstances();
  const createPol = useCreatePolicyInstance();
  const updatePol = useUpdatePolicyInstance();
  const deletePol = useDeletePolicyInstance();

  const [dialog, setDialog] = useState<DialogState>(null);
  const [alias, setAlias] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [content, setContent] = useState(SAMPLE_CONTENT);

  const policies = data?.data ?? [];

  function openCreate() {
    setAlias("");
    setDisplayName("");
    setContent(SAMPLE_CONTENT);
    setDialog({ type: "create" });
  }
  function openEdit(p: PolicyInstance) {
    setAlias(p.alias);
    setDisplayName(p.display_name);
    setContent(p.content);
    setDialog({ type: "edit", policy: p });
  }

  function handleSave() {
    if (!/^[a-z][a-z0-9_]*$/.test(alias.trim())) {
      toast.error("alias は英小文字で始まり、英小文字・数字・_ のみ使えます");
      return;
    }
    const params = { alias: alias.trim(), display_name: displayName.trim(), content };
    const opts = {
      onSuccess: () => {
        toast.success("保存しました");
        setDialog(null);
      },
      onError: (err: Error) => toast.error(`保存に失敗しました: ${err.message}`),
    };
    if (dialog?.type === "edit") {
      updatePol.mutate({ id: dialog.policy.id, params }, opts);
    } else {
      createPol.mutate(params, opts);
    }
  }

  function handleDelete() {
    if (dialog?.type !== "delete") return;
    deletePol.mutate(dialog.policy.id, {
      onSuccess: () => {
        toast.success("削除しました");
        setDialog(null);
      },
      onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
    });
  }

  const isSaving = createPol.isPending || updatePol.isPending;

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="ポリシー"
        description="検査結果 → アクションのルール群。ルーティングから参照する再利用可能な部品です。"
        count={data ? policies.length : undefined}
        actions={
          <Button onClick={openCreate}>
            <Plus className="h-4 w-4 mr-2" />
            ポリシーを追加
          </Button>
        }
      />

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          ポリシーの取得に失敗しました。
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
                <TableHead>alias</TableHead>
                <TableHead>表示名</TableHead>
                <TableHead>アクション</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {policies.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={3} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <ScrollText className="h-8 w-8 text-gray-300" />
                      ポリシーがありません。「ポリシーを追加」から作成してください。
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                policies.map((p) => (
                  <TableRow key={p.id}>
                    <TableCell className="font-mono text-sm">{p.alias}</TableCell>
                    <TableCell className="text-sm text-gray-700">{p.display_name}</TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button variant="outline" size="sm" onClick={() => openEdit(p)}>
                          <Pencil className="h-3.5 w-3.5 mr-1" />編集
                        </Button>
                        <Button variant="destructive" size="sm" onClick={() => setDialog({ type: "delete", policy: p })}>
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

      <Dialog open={dialog?.type === "create" || dialog?.type === "edit"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>{dialog?.type === "edit" ? "ポリシーを編集" : "ポリシーを追加"}</DialogTitle>
          <DialogDescription>
            条件では検査ワーカーインスタンスの alias を参照します（例: av_internal.detected == true）。
          </DialogDescription>
        </DialogHeader>
        <div className="px-5 py-4 space-y-3 max-h-[64vh] overflow-y-auto">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">alias *</label>
              <Input placeholder="standard_inbound" value={alias} onChange={(e) => setAlias(e.target.value)} />
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">表示名</label>
              <Input placeholder="標準受信ポリシー" value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
            </div>
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">ルール定義（YAML）</label>
            <textarea
              className="w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs
                         focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              rows={14}
              spellCheck={false}
              value={content}
              onChange={(e) => setContent(e.target.value)}
            />
            <p className="text-xs text-gray-400">
              lists と rules を定義します。メール消失を防ぐため、最後に condition: "true" のフォールバックを置くことを推奨。
            </p>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button onClick={handleSave} disabled={isSaving}>{isSaving ? "保存中…" : "保存する"}</Button>
        </DialogFooter>
      </Dialog>

      <Dialog open={dialog?.type === "delete"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>ポリシーを削除しますか？</DialogTitle>
          <DialogDescription>
            {dialog?.type === "delete" && (
              <>「{dialog.policy.display_name || dialog.policy.alias}」を削除します。ルーティングから参照されている場合、次回の設定検証でエラーになります。</>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button variant="destructive" onClick={handleDelete} disabled={deletePol.isPending}>
            {deletePol.isPending ? "削除中…" : "削除する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
