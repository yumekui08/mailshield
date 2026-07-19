import { useState } from "react";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, Boxes } from "lucide-react";
import {
  useWorkerInstances,
  useCreateWorkerInstance,
  useUpdateWorkerInstance,
  useDeleteWorkerInstance,
} from "../hooks/useConfig";
import { PageHeader } from "../components/PageHeader";
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
import type { WorkerInstance, WorkerKind } from "../types";

// コード側で登録済みの代表的なワーカー型（datalist 候補・自由入力も可）
const KNOWN_TYPES = [
  "av-worker", "dlp-worker", "header-inspector", "url-worker", "qr-worker",
  "attachment-inspector", "sanitize-worker", "macro-strip", "url-rewrite-worker",
  "disclaimer-worker", "filesep-worker", "arc-sealer",
];

const kindLabel: Record<WorkerKind, string> = {
  inspect: "検査（inspect）",
  transform: "変換（transform）",
};

type DialogState =
  | { type: "create" }
  | { type: "edit"; instance: WorkerInstance }
  | { type: "delete"; instance: WorkerInstance }
  | null;

interface FormState {
  alias: string;
  display_name: string;
  worker_type: string;
  kind: WorkerKind;
  configText: string;
  default_timeout_seconds: number;
  is_enabled: boolean;
}

const emptyForm: FormState = {
  alias: "",
  display_name: "",
  worker_type: "",
  kind: "inspect",
  configText: "{}",
  default_timeout_seconds: 30,
  is_enabled: true,
};

export function WorkerInstancesPage() {
  const { data, isLoading, isError } = useWorkerInstances();
  const createInst = useCreateWorkerInstance();
  const updateInst = useUpdateWorkerInstance();
  const deleteInst = useDeleteWorkerInstance();

  const [dialog, setDialog] = useState<DialogState>(null);
  const [form, setForm] = useState<FormState>(emptyForm);

  const instances = data?.data ?? [];

  function openCreate() {
    setForm(emptyForm);
    setDialog({ type: "create" });
  }
  function openEdit(inst: WorkerInstance) {
    setForm({
      alias: inst.alias,
      display_name: inst.display_name,
      worker_type: inst.worker_type,
      kind: inst.kind,
      configText: JSON.stringify(inst.config ?? {}, null, 2),
      default_timeout_seconds: inst.default_timeout_seconds,
      is_enabled: inst.is_enabled,
    });
    setDialog({ type: "edit", instance: inst });
  }

  function handleSave() {
    if (!/^[a-z][a-z0-9_]*$/.test(form.alias.trim())) {
      toast.error("alias は英小文字で始まり、英小文字・数字・_ のみ使えます");
      return;
    }
    if (!form.worker_type.trim()) {
      toast.error("ワーカー型は必須です");
      return;
    }
    let config: Record<string, unknown>;
    try {
      config = JSON.parse(form.configText || "{}");
    } catch {
      toast.error("設定（config）が正しい JSON ではありません");
      return;
    }
    const params = {
      alias: form.alias.trim(),
      display_name: form.display_name.trim(),
      worker_type: form.worker_type.trim(),
      kind: form.kind,
      config,
      default_timeout_seconds: form.default_timeout_seconds,
      is_enabled: form.is_enabled,
    };
    const opts = {
      onSuccess: () => {
        toast.success("保存しました");
        setDialog(null);
      },
      onError: (err: Error) => toast.error(`保存に失敗しました: ${err.message}`),
    };
    if (dialog?.type === "edit") {
      updateInst.mutate({ id: dialog.instance.id, params }, opts);
    } else {
      createInst.mutate(params, opts);
    }
  }

  function handleDelete() {
    if (dialog?.type !== "delete") return;
    deleteInst.mutate(dialog.instance.id, {
      onSuccess: () => {
        toast.success("削除しました");
        setDialog(null);
      },
      onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
    });
  }

  const isSaving = createInst.isPending || updateInst.isPending;

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="ワーカーインスタンス"
        description="ワーカー型＋設定＋名前の再利用可能な部品。ルーティングから alias で参照します。"
        count={data ? instances.length : undefined}
        actions={
          <Button onClick={openCreate}>
            <Plus className="h-4 w-4 mr-2" />
            インスタンスを追加
          </Button>
        }
      />

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          ワーカーインスタンスの取得に失敗しました。
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
                <TableHead>ワーカー型</TableHead>
                <TableHead>種別</TableHead>
                <TableHead>状態</TableHead>
                <TableHead>アクション</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {instances.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <Boxes className="h-8 w-8 text-gray-300" />
                      インスタンスがありません。「インスタンスを追加」から作成してください。
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                instances.map((inst) => (
                  <TableRow key={inst.id}>
                    <TableCell className="font-mono text-sm">{inst.alias}</TableCell>
                    <TableCell className="text-sm text-gray-700">{inst.display_name}</TableCell>
                    <TableCell className="text-sm text-gray-500 font-mono">{inst.worker_type}</TableCell>
                    <TableCell>
                      <Badge variant={inst.kind === "inspect" ? "blue" : "green"}>
                        {inst.kind === "inspect" ? "検査" : "変換"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Badge variant={inst.is_enabled ? "green" : "default"}>
                        {inst.is_enabled ? "有効" : "無効"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button variant="outline" size="sm" onClick={() => openEdit(inst)}>
                          <Pencil className="h-3.5 w-3.5 mr-1" />編集
                        </Button>
                        <Button variant="destructive" size="sm" onClick={() => setDialog({ type: "delete", instance: inst })}>
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
          <DialogTitle>{dialog?.type === "edit" ? "インスタンスを編集" : "インスタンスを追加"}</DialogTitle>
          <DialogDescription>
            同じワーカー型から用途別のインスタンスを複数作れます（例: 内部向け／外部向け）。
          </DialogDescription>
        </DialogHeader>
        <div className="px-5 py-4 space-y-3 max-h-[60vh] overflow-y-auto">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">alias *</label>
              <Input
                placeholder="av_internal"
                value={form.alias}
                onChange={(e) => setForm({ ...form, alias: e.target.value })}
              />
              <p className="text-xs text-gray-400">条件式・検査結果のキー。英小文字始まり・変更非推奨。</p>
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">表示名</label>
              <Input
                placeholder="内部向けウイルス検査"
                value={form.display_name}
                onChange={(e) => setForm({ ...form, display_name: e.target.value })}
              />
            </div>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">ワーカー型 *</label>
              <Input
                list="worker-types"
                placeholder="av-worker"
                value={form.worker_type}
                onChange={(e) => setForm({ ...form, worker_type: e.target.value })}
              />
              <datalist id="worker-types">
                {KNOWN_TYPES.map((t) => <option key={t} value={t} />)}
              </datalist>
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">種別</label>
              <Select value={form.kind} onChange={(e) => setForm({ ...form, kind: e.target.value as WorkerKind })}>
                <option value="inspect">{kindLabel.inspect}</option>
                <option value="transform">{kindLabel.transform}</option>
              </Select>
            </div>
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">設定（JSON）</label>
            <textarea
              className="w-full rounded-md border border-gray-300 px-3 py-2 font-mono text-xs
                         focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
              rows={8}
              spellCheck={false}
              value={form.configText}
              onChange={(e) => setForm({ ...form, configText: e.target.value })}
            />
            <p className="text-xs text-gray-400">ワーカー型ごとの設定を JSON で記述します（${"{"}VAR{"}"} 参照可）。</p>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">既定タイムアウト（秒）</label>
              <Input
                type="number"
                min={0}
                value={form.default_timeout_seconds}
                onChange={(e) => setForm({ ...form, default_timeout_seconds: Number(e.target.value) })}
              />
              <p className="text-xs text-gray-400">ルーティング側で上書きできます。</p>
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">状態</label>
              <Select
                value={String(form.is_enabled)}
                onChange={(e) => setForm({ ...form, is_enabled: e.target.value === "true" })}
              >
                <option value="true">有効</option>
                <option value="false">無効</option>
              </Select>
            </div>
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
          <DialogTitle>インスタンスを削除しますか？</DialogTitle>
          <DialogDescription>
            {dialog?.type === "delete" && (
              <>「{dialog.instance.display_name || dialog.instance.alias}」を削除します。ルーティングから参照されている場合、次回の設定検証でエラーになります。</>
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button variant="destructive" onClick={handleDelete} disabled={deleteInst.isPending}>
            {deleteInst.isPending ? "削除中…" : "削除する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
