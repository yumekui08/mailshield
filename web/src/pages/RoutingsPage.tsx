import { useState } from "react";
import { toast } from "sonner";
import { Plus, Pencil, Trash2, Route as RouteIcon, ArrowUp, ArrowDown, X, Lock } from "lucide-react";
import {
  useRoutings,
  useCreateRouting,
  useUpdateRouting,
  useDeleteRouting,
  useWorkerInstances,
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
import type { Routing, WorkerBinding, WorkerInstance } from "../types";

type DialogState =
  | { type: "create" }
  | { type: "edit"; routing: Routing }
  | { type: "delete"; routing: Routing }
  | null;

interface FormState {
  name: string;
  priority: number;
  match_expr: string;
  policy_ref: string;
  is_enabled: boolean;
  inspect: WorkerBinding[];
  transform: WorkerBinding[];
}

const emptyForm: FormState = {
  name: "",
  priority: 100,
  match_expr: "true",
  policy_ref: "",
  is_enabled: true,
  inspect: [],
  transform: [],
};

// BindingEditor は inspect / transform のワーカーインスタンス束ねを編集する。
function BindingEditor({
  label,
  kind,
  bindings,
  instances,
  ordered,
  onChange,
}: {
  label: string;
  kind: "inspect" | "transform";
  bindings: WorkerBinding[];
  instances: WorkerInstance[];
  ordered: boolean;
  onChange: (next: WorkerBinding[]) => void;
}) {
  const options = instances.filter((i) => i.kind === kind);

  function update(idx: number, patch: Partial<WorkerBinding>) {
    onChange(bindings.map((b, i) => (i === idx ? { ...b, ...patch } : b)));
  }
  function remove(idx: number) {
    onChange(bindings.filter((_, i) => i !== idx));
  }
  function move(idx: number, dir: -1 | 1) {
    const j = idx + dir;
    if (j < 0 || j >= bindings.length) return;
    const next = [...bindings];
    [next[idx], next[j]] = [next[j], next[idx]];
    onChange(next);
  }
  function add() {
    onChange([...bindings, { alias: options[0]?.alias ?? "", enabled: true }]);
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <span className="text-sm font-medium text-gray-700">
          {label}
          {ordered && <span className="ml-1 text-xs text-gray-400">（上から順に直列実行）</span>}
        </span>
        <Button variant="outline" size="sm" onClick={add} disabled={options.length === 0}>
          <Plus className="h-3.5 w-3.5 mr-1" />追加
        </Button>
      </div>
      {options.length === 0 && (
        <p className="text-xs text-amber-600">
          {kind === "inspect" ? "検査" : "変換"}種別のワーカーインスタンスがありません。先に作成してください。
        </p>
      )}
      {bindings.length === 0 ? (
        <p className="text-xs text-gray-400">なし</p>
      ) : (
        <div className="space-y-1.5">
          {bindings.map((b, idx) => (
            <div key={idx} className="flex items-center gap-2 rounded border border-gray-200 px-2 py-1.5">
              {ordered && (
                <div className="flex flex-col">
                  <button className="text-gray-400 hover:text-gray-700" onClick={() => move(idx, -1)} aria-label="上へ">
                    <ArrowUp className="h-3 w-3" />
                  </button>
                  <button className="text-gray-400 hover:text-gray-700" onClick={() => move(idx, 1)} aria-label="下へ">
                    <ArrowDown className="h-3 w-3" />
                  </button>
                </div>
              )}
              <Select
                value={b.alias}
                onChange={(e) => update(idx, { alias: e.target.value })}
                className="flex-1"
              >
                {/* 現在値が候補に無い場合も表示できるようにする */}
                {!options.some((o) => o.alias === b.alias) && b.alias && (
                  <option value={b.alias}>{b.alias}（未定義）</option>
                )}
                {options.map((o) => (
                  <option key={o.id} value={o.alias}>
                    {o.alias}{o.display_name ? `（${o.display_name}）` : ""}
                  </option>
                ))}
              </Select>
              <label className="flex items-center gap-1 text-xs text-gray-600">
                <input
                  type="checkbox"
                  checked={b.enabled}
                  onChange={(e) => update(idx, { enabled: e.target.checked })}
                />
                有効
              </label>
              <Input
                type="number"
                min={0}
                placeholder="既定"
                value={b.timeout_seconds ?? ""}
                onChange={(e) =>
                  update(idx, { timeout_seconds: e.target.value === "" ? null : Number(e.target.value) })
                }
                className="w-20"
                title="タイムアウト秒（空ならインスタンス既定）"
              />
              <button className="text-gray-400 hover:text-red-500" onClick={() => remove(idx)} aria-label="削除">
                <X className="h-4 w-4" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export function RoutingsPage() {
  const { data, isLoading, isError } = useRoutings();
  const { data: instData } = useWorkerInstances();
  const createRt = useCreateRouting();
  const updateRt = useUpdateRouting();
  const deleteRt = useDeleteRouting();

  const [dialog, setDialog] = useState<DialogState>(null);
  const [form, setForm] = useState<FormState>(emptyForm);

  const routings = data?.data ?? [];
  const instances = instData?.data ?? [];
  const editingCatchAll = dialog?.type === "edit" && dialog.routing.is_catchall;

  function openCreate() {
    setForm(emptyForm);
    setDialog({ type: "create" });
  }
  function openEdit(rt: Routing) {
    setForm({
      name: rt.name,
      priority: rt.priority,
      match_expr: rt.match_expr,
      policy_ref: rt.policy_ref,
      is_enabled: rt.is_enabled,
      inspect: rt.inspect ?? [],
      transform: rt.transform ?? [],
    });
    setDialog({ type: "edit", routing: rt });
  }

  function handleSave() {
    if (!form.match_expr.trim()) {
      toast.error("マッチ条件は必須です（すべてに一致させるなら true）");
      return;
    }
    const params = {
      name: form.name.trim(),
      priority: form.priority,
      match_expr: form.match_expr.trim(),
      is_enabled: form.is_enabled,
      policy_ref: form.policy_ref.trim(),
      inspect: form.inspect,
      transform: form.transform,
    };
    const opts = {
      onSuccess: () => {
        toast.success("保存しました");
        setDialog(null);
      },
      onError: (err: Error) => toast.error(`保存に失敗しました: ${err.message}`),
    };
    if (dialog?.type === "edit") {
      updateRt.mutate({ id: dialog.routing.id, params }, opts);
    } else {
      createRt.mutate(params, opts);
    }
  }

  function handleDelete() {
    if (dialog?.type !== "delete") return;
    deleteRt.mutate(dialog.routing.id, {
      onSuccess: () => {
        toast.success("削除しました");
        setDialog(null);
      },
      onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
    });
  }

  const isSaving = createRt.isPending || updateRt.isPending;

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="ルーティング"
        description="メールがどの検査・変換・ポリシーを通るかを priority 昇順の first-match で決めます。"
        count={data ? routings.length : undefined}
        actions={
          <Button onClick={openCreate}>
            <Plus className="h-4 w-4 mr-2" />
            ルーティングを追加
          </Button>
        }
      />

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          ルーティングの取得に失敗しました。
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-surface overflow-hidden" data-help="routings-table">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {[...Array(3)].map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-16">優先度</TableHead>
                <TableHead>名前</TableHead>
                <TableHead>マッチ条件</TableHead>
                <TableHead>ポリシー</TableHead>
                <TableHead>検査 / 変換</TableHead>
                <TableHead>状態</TableHead>
                <TableHead>アクション</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {routings.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <RouteIcon className="h-8 w-8 text-gray-300" />
                      ルーティングがありません
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                routings.map((rt) => (
                  <TableRow key={rt.id}>
                    <TableCell className="text-sm tabular-nums text-gray-500">
                      {rt.is_catchall ? "—" : rt.priority}
                    </TableCell>
                    <TableCell className="text-sm font-medium">
                      <div className="flex items-center gap-1.5">
                        {rt.name || <span className="text-gray-400">（無名）</span>}
                        {rt.is_catchall && (
                          <Badge variant="default" className="gap-1">
                            <Lock className="h-3 w-3" />catch-all
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="text-sm font-mono text-gray-600 max-w-64 truncate">{rt.match_expr}</TableCell>
                    <TableCell className="text-sm text-gray-600">{rt.policy_ref || "—"}</TableCell>
                    <TableCell className="text-sm text-gray-500 tabular-nums">
                      {rt.inspect?.length ?? 0} / {rt.transform?.length ?? 0}
                    </TableCell>
                    <TableCell>
                      <Badge variant={rt.is_enabled ? "green" : "default"}>
                        {rt.is_enabled ? "有効" : "無効"}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button variant="outline" size="sm" onClick={() => openEdit(rt)}>
                          <Pencil className="h-3.5 w-3.5 mr-1" />編集
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          disabled={rt.is_catchall}
                          title={rt.is_catchall ? "catch-all は削除できません" : undefined}
                          onClick={() => setDialog({ type: "delete", routing: rt })}
                        >
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
          <DialogTitle>{dialog?.type === "edit" ? "ルーティングを編集" : "ルーティングを追加"}</DialogTitle>
          <DialogDescription>
            {editingCatchAll
              ? "catch-all（デフォルト）は必ず最後に全メールを受けます。マッチ条件・優先度は固定です。"
              : "priority が小さいほど先に評価され、最初に一致した 1 つだけを通します。"}
          </DialogDescription>
        </DialogHeader>
        <div className="px-5 py-4 space-y-3 max-h-[64vh] overflow-y-auto">
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">名前</label>
              <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="inbound" />
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">優先度</label>
              <Input
                type="number"
                value={form.priority}
                onChange={(e) => setForm({ ...form, priority: Number(e.target.value) })}
                disabled={editingCatchAll}
              />
            </div>
          </div>
          <div className="space-y-1">
            <label className="text-sm font-medium text-gray-700">マッチ条件</label>
            <Input
              className="font-mono text-sm"
              value={form.match_expr}
              onChange={(e) => setForm({ ...form, match_expr: e.target.value })}
              disabled={editingCatchAll}
              placeholder='mail.to endswith ${INTERNAL_DOMAIN}'
            />
            <p className="text-xs text-gray-400">
              ワーカー実行前に評価されるため、封筒・ヘッダのみ参照可（スコアはポリシー側で）。${"{"}VAR{"}"} 参照可。
            </p>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">ポリシー名</label>
              <Input
                value={form.policy_ref}
                onChange={(e) => setForm({ ...form, policy_ref: e.target.value })}
                placeholder="標準受信ポリシー"
              />
            </div>
            <div className="space-y-1">
              <label className="text-sm font-medium text-gray-700">状態</label>
              <Select
                value={String(form.is_enabled)}
                onChange={(e) => setForm({ ...form, is_enabled: e.target.value === "true" })}
                disabled={editingCatchAll}
              >
                <option value="true">有効</option>
                <option value="false">無効</option>
              </Select>
            </div>
          </div>
          <BindingEditor
            label="検査ワーカー（inspect・並列）"
            kind="inspect"
            bindings={form.inspect}
            instances={instances}
            ordered={false}
            onChange={(next) => setForm({ ...form, inspect: next })}
          />
          <BindingEditor
            label="変換ワーカー（transform）"
            kind="transform"
            bindings={form.transform}
            instances={instances}
            ordered
            onChange={(next) => setForm({ ...form, transform: next })}
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button onClick={handleSave} disabled={isSaving}>{isSaving ? "保存中…" : "保存する"}</Button>
        </DialogFooter>
      </Dialog>

      {/* 削除確認 */}
      <Dialog open={dialog?.type === "delete"} onClose={() => setDialog(null)}>
        <DialogHeader>
          <DialogTitle>ルーティングを削除しますか？</DialogTitle>
          <DialogDescription>
            {dialog?.type === "delete" && <>「{dialog.routing.name || "（無名）"}」を削除します。</>}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setDialog(null)}>キャンセル</Button>
          <Button variant="destructive" onClick={handleDelete} disabled={deleteRt.isPending}>
            {deleteRt.isPending ? "削除中…" : "削除する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
