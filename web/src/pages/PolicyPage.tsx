import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Plus,
  Pencil,
  Trash2,
  ArrowUp,
  ArrowDown,
  Save,
  ScrollText,
  History,
  LayoutTemplate,
  RotateCcw,
} from "lucide-react";
import { usePolicyRoutes, useUpdatePolicyRoute } from "../hooks/usePolicy";
import { useMe } from "../hooks/useAuth";
import { ApiError, getPolicyVersions, rollbackPolicy } from "../lib/api";
import { POLICY_TEMPLATES } from "../lib/policyTemplates";
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
import type {
  PolicyRoute,
  PolicyRule,
  PolicyActionSpec,
  PolicyHits,
} from "../types";

const TERMINAL_ACTIONS = [
  "deliver",
  "redirect",
  "reject",
  "quarantine",
  "approval",
  "delay",
];
const NONTERMINAL_ACTIONS = [
  "add_subject_prefix",
  "add_header",
  "remove_header",
  "strip_attachments",
  "log_only",
];

function actionBadgeVariant(
  type: string
): "green" | "yellow" | "red" | "blue" | "default" {
  if (type === "deliver") return "green";
  if (type === "quarantine") return "yellow";
  if (type === "reject") return "red";
  if (type === "approval" || type === "delay" || type === "redirect") return "blue";
  return "default";
}

// ルールを常に actions 配列で扱えるよう正規化する。
function normalizeRule(r: PolicyRule): PolicyRule {
  if (r.actions && r.actions.length > 0) return { ...r };
  if (r.action) {
    return {
      ...r,
      actions: [
        {
          type: r.action,
          destination: r.destination,
          delay_minutes: r.delay_minutes,
        },
      ],
      action: undefined,
      destination: undefined,
      delay_minutes: undefined,
    };
  }
  return { ...r, actions: [] };
}

// 保存時: 単一の単純な終端アクションは action: に畳んでファイルをすっきり保つ。
function denormalizeRule(r: PolicyRule): PolicyRule {
  const acts = r.actions ?? [];
  if (
    acts.length === 1 &&
    TERMINAL_ACTIONS.includes(acts[0].type) &&
    !acts[0].name &&
    !acts[0].value
  ) {
    const a = acts[0];
    const out: PolicyRule = {
      name: r.name,
      condition: r.condition,
      action: a.type,
    };
    if (r.description) out.description = r.description;
    if (r.enabled === false) out.enabled = false;
    if (r.priority) out.priority = r.priority;
    if (r.tags && r.tags.length) out.tags = r.tags;
    if (a.destination) out.destination = a.destination;
    if (a.delay_minutes) out.delay_minutes = a.delay_minutes;
    return out;
  }
  return r;
}

function actionSummary(r: PolicyRule): PolicyActionSpec[] {
  return normalizeRule(r).actions ?? [];
}

export function PolicyPage() {
  const { data: me } = useMe();
  const { data, isLoading, isError } = usePolicyRoutes();
  const update = useUpdatePolicyRoute();

  const canEdit = me?.role === "admin";
  const [selectedDir, setSelectedDir] = useState<string>("");
  // 編集中のドラフト（ルート dir → ルール配列）。未編集なら undefined。
  const [draft, setDraft] = useState<Record<string, PolicyRule[]>>({});
  const [editing, setEditing] = useState<{ index: number; rule: PolicyRule } | null>(
    null
  );
  const [showHistory, setShowHistory] = useState(false);
  const [showTemplates, setShowTemplates] = useState(false);

  const routes = data?.routes ?? [];
  const hits: PolicyHits = data?.hits ?? {};

  const activeDir = selectedDir || routes[0]?.dir || "";
  const activeRoute: PolicyRoute | undefined = routes.find(
    (r) => r.dir === activeDir
  );

  const rules: PolicyRule[] = useMemo(() => {
    if (draft[activeDir]) return draft[activeDir];
    return (activeRoute?.policy.rules ?? []).map(normalizeRule);
  }, [draft, activeDir, activeRoute]);

  const dirty = draft[activeDir] !== undefined;

  function setRules(next: PolicyRule[]) {
    setDraft((d) => ({ ...d, [activeDir]: next }));
  }

  function move(index: number, dir: -1 | 1) {
    const next = [...rules];
    const j = index + dir;
    if (j < 0 || j >= next.length) return;
    [next[index], next[j]] = [next[j], next[index]];
    setRules(next);
  }

  function remove(index: number) {
    setRules(rules.filter((_, i) => i !== index));
  }

  function toggleEnabled(index: number) {
    const next = [...rules];
    const cur = next[index].enabled;
    next[index] = { ...next[index], enabled: cur === false ? true : false };
    setRules(next);
  }

  function insertTemplate(templateRules: PolicyRule[]) {
    const normalized = templateRules.map(normalizeRule);
    // フォールバック（condition:"true"）の手前に挿入する
    const idx = rules.findIndex((r) => r.condition.trim() === "true");
    const next =
      idx >= 0
        ? [...rules.slice(0, idx), ...normalized, ...rules.slice(idx)]
        : [...rules, ...normalized];
    setRules(next);
    setShowTemplates(false);
    toast.success("テンプレートを挿入しました。内容を確認して保存してください");
  }

  function saveRule(rule: PolicyRule) {
    if (!editing) return;
    const next = [...rules];
    if (editing.index === -1) next.push(rule);
    else next[editing.index] = rule;
    setRules(next);
    setEditing(null);
  }

  function discard() {
    setDraft((d) => {
      const n = { ...d };
      delete n[activeDir];
      return n;
    });
  }

  function save() {
    const doc = {
      lists: activeRoute?.policy.lists,
      rules: rules.map(denormalizeRule),
    };
    update.mutate(
      { dir: activeDir, doc },
      {
        onSuccess: () => {
          toast.success("ポリシーを保存し、smtp-gateway に反映しました");
          discard();
        },
        onError: (e) => {
          const msg = e instanceof ApiError ? extractMessage(e.message) : String(e);
          toast.error(msg, { duration: 8000 });
        },
      }
    );
  }

  if (isLoading) return <Skeleton className="h-64 w-full" />;
  if (isError)
    return <div className="text-sm text-red-600">ポリシーの読み込みに失敗しました。</div>;

  return (
    <div className="space-y-4">
      <PageHeader
        title="ポリシー"
        description="ルートごとの検査結果 → アクションのルールを編集します。保存すると即座に反映されます。"
      />

      {/* ルート切替タブ */}
      <div className="flex flex-wrap gap-1 border-b border-gray-200">
        {routes.map((r) => (
          <button
            key={r.dir}
            onClick={() => setSelectedDir(r.dir)}
            className={`px-3 py-1.5 text-sm -mb-px border-b-2 ${
              r.dir === activeDir
                ? "border-blue-600 text-blue-700 font-medium"
                : "border-transparent text-gray-500 hover:text-gray-800"
            }`}
          >
            {r.name}
            {draft[r.dir] !== undefined && (
              <span className="ml-1 text-amber-600" title="未保存の変更">
                ●
              </span>
            )}
          </button>
        ))}
      </div>

      {activeRoute && (
        <div className="flex items-center justify-between">
          <div className="text-xs text-gray-500">
            ディレクトリ: <code>{activeRoute.dir}</code> / 方向:{" "}
            <Badge variant="default">{activeRoute.direction}</Badge>
          </div>
          {canEdit && (
            <div className="flex gap-2">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setShowHistory(true)}
              >
                <History className="mr-1 h-3.5 w-3.5" />
                履歴
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowTemplates(true)}
              >
                <LayoutTemplate className="mr-1 h-3.5 w-3.5" />
                テンプレート
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditing({ index: -1, rule: emptyRule() })}
              >
                <Plus className="mr-1 h-3.5 w-3.5" />
                ルール追加
              </Button>
              {dirty && (
                <>
                  <Button variant="ghost" size="sm" onClick={discard}>
                    変更を破棄
                  </Button>
                  <Button size="sm" onClick={save} disabled={update.isPending}>
                    <Save className="mr-1 h-3.5 w-3.5" />
                    保存して反映
                  </Button>
                </>
              )}
            </div>
          )}
        </div>
      )}

      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-10">#</TableHead>
            <TableHead>ルール名</TableHead>
            <TableHead>条件</TableHead>
            <TableHead>アクション</TableHead>
            <TableHead className="w-16 text-right">ヒット</TableHead>
            <TableHead className="w-16">有効</TableHead>
            {canEdit && <TableHead className="w-28 text-right">操作</TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rules.map((r, i) => {
            const hitCount = hits[activeRoute?.name ?? ""]?.[r.name];
            const disabled = r.enabled === false;
            return (
              <TableRow key={`${r.name}-${i}`} className={disabled ? "opacity-50" : ""}>
                <TableCell className="text-gray-400">{i + 1}</TableCell>
                <TableCell>
                  <div className="font-medium">{r.name}</div>
                  {r.description && (
                    <div className="text-xs text-gray-500">{r.description}</div>
                  )}
                  {r.tags && r.tags.length > 0 && (
                    <div className="mt-0.5 flex flex-wrap gap-1">
                      {r.tags.map((t) => (
                        <span
                          key={t}
                          className="text-[10px] text-gray-500 bg-gray-100 px-1 py-px"
                        >
                          {t}
                        </span>
                      ))}
                    </div>
                  )}
                </TableCell>
                <TableCell>
                  <code className="text-xs text-gray-600">{r.condition}</code>
                </TableCell>
                <TableCell>
                  <div className="flex flex-wrap gap-1">
                    {actionSummary(r).map((a, k) => (
                      <Badge key={k} variant={actionBadgeVariant(a.type)}>
                        {a.type}
                      </Badge>
                    ))}
                  </div>
                </TableCell>
                <TableCell className="text-right tabular-nums text-gray-600">
                  {hitCount ?? "—"}
                </TableCell>
                <TableCell>
                  {canEdit ? (
                    <button
                      onClick={() => toggleEnabled(i)}
                      className="text-xs underline text-gray-500 hover:text-gray-800"
                    >
                      {disabled ? "無効" : "有効"}
                    </button>
                  ) : disabled ? (
                    "無効"
                  ) : (
                    "有効"
                  )}
                </TableCell>
                {canEdit && (
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-0.5">
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => move(i, -1)}
                        disabled={i === 0}
                        aria-label="上へ"
                      >
                        <ArrowUp className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => move(i, 1)}
                        disabled={i === rules.length - 1}
                        aria-label="下へ"
                      >
                        <ArrowDown className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setEditing({ index: i, rule: r })}
                        aria-label="編集"
                      >
                        <Pencil className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => remove(i)}
                        aria-label="削除"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                )}
              </TableRow>
            );
          })}
          {rules.length === 0 && (
            <TableRow>
              <TableCell colSpan={canEdit ? 7 : 6} className="text-center text-gray-500">
                ルールがありません
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>

      <p className="flex items-start gap-1.5 text-xs text-gray-500">
        <ScrollText className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        ルールは上から順に評価され、最初の終端アクション（deliver / reject / quarantine /
        approval / delay）で停止します。非終端アクション（タグ付け等）は次のルールへ続行します。
        末尾に <code>condition: "true"</code> のフォールバックルールを必ず 1 つ置いてください。
      </p>

      {editing && (
        <RuleDialog
          rule={editing.rule}
          isNew={editing.index === -1}
          onClose={() => setEditing(null)}
          onSave={saveRule}
        />
      )}

      {showTemplates && (
        <TemplatesDialog
          onClose={() => setShowTemplates(false)}
          onInsert={insertTemplate}
        />
      )}

      {showHistory && activeRoute && (
        <HistoryDialog
          dir={activeRoute.dir}
          onClose={() => setShowHistory(false)}
        />
      )}
    </div>
  );
}

function TemplatesDialog({
  onClose,
  onInsert,
}: {
  onClose: () => void;
  onInsert: (rules: PolicyRule[]) => void;
}) {
  return (
    <Dialog open onClose={onClose}>
      <DialogHeader>
        <DialogTitle>シナリオテンプレート</DialogTitle>
        <DialogDescription>
          定型ルールをフォールバックの手前に挿入します。挿入後に内容を調整して保存してください。
        </DialogDescription>
      </DialogHeader>
      <div className="space-y-2">
        {POLICY_TEMPLATES.map((t) => (
          <div
            key={t.id}
            className="flex items-start justify-between gap-3 border border-gray-200 p-2"
          >
            <div>
              <div className="text-sm font-medium">{t.name}</div>
              <div className="text-xs text-gray-500">{t.description}</div>
            </div>
            <Button variant="outline" size="sm" onClick={() => onInsert(t.rules)}>
              <Plus className="mr-1 h-3 w-3" />
              挿入
            </Button>
          </div>
        ))}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          閉じる
        </Button>
      </DialogFooter>
    </Dialog>
  );
}

function HistoryDialog({ dir, onClose }: { dir: string; onClose: () => void }) {
  const qc = useQueryClient();
  const { data, isLoading } = useQuery({
    queryKey: ["policy", "versions", dir],
    queryFn: () => getPolicyVersions(dir),
  });
  const rollback = useMutation({
    mutationFn: (versionId: string) => rollbackPolicy(dir, versionId),
    onSuccess: () => {
      toast.success("以前のバージョンに戻し、smtp-gateway に反映しました");
      qc.invalidateQueries({ queryKey: ["policy", "routes"] });
      qc.invalidateQueries({ queryKey: ["policy", "versions", dir] });
      onClose();
    },
    onError: (e) => {
      const msg = e instanceof ApiError ? extractMessage(e.message) : String(e);
      toast.error(msg, { duration: 8000 });
    },
  });
  const versions = data?.versions ?? [];

  return (
    <Dialog open onClose={onClose}>
      <DialogHeader>
        <DialogTitle>変更履歴（{dir}）</DialogTitle>
        <DialogDescription>
          各行は「その時点より前の内容」です。復元すると現在の内容も履歴に残ります。
        </DialogDescription>
      </DialogHeader>
      <div className="max-h-96 space-y-1 overflow-y-auto">
        {isLoading && <Skeleton className="h-20 w-full" />}
        {!isLoading && versions.length === 0 && (
          <div className="text-sm text-gray-500">履歴はまだありません。</div>
        )}
        {versions.map((v) => (
          <div
            key={v.id}
            className="flex items-center justify-between gap-3 border border-gray-200 px-2 py-1.5"
          >
            <div className="text-xs">
              <div className="tabular-nums">
                {new Date(v.created_at).toLocaleString()}
              </div>
              <div className="text-gray-500">{v.actor_email ?? "—"}</div>
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                if (confirm("この時点の内容に復元しますか？")) rollback.mutate(v.id);
              }}
              disabled={rollback.isPending}
            >
              <RotateCcw className="mr-1 h-3 w-3" />
              復元
            </Button>
          </div>
        ))}
      </div>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          閉じる
        </Button>
      </DialogFooter>
    </Dialog>
  );
}

function emptyRule(): PolicyRule {
  return { name: "", condition: "true", actions: [{ type: "deliver" }] };
}

function extractMessage(raw: string): string {
  try {
    const j = JSON.parse(raw);
    return j?.error?.message ?? raw;
  } catch {
    return raw;
  }
}

function RuleDialog({
  rule,
  isNew,
  onClose,
  onSave,
}: {
  rule: PolicyRule;
  isNew: boolean;
  onClose: () => void;
  onSave: (r: PolicyRule) => void;
}) {
  const [name, setName] = useState(rule.name);
  const [description, setDescription] = useState(rule.description ?? "");
  const [priority, setPriority] = useState(rule.priority ?? 0);
  const [tags, setTags] = useState((rule.tags ?? []).join(", "));
  const [condition, setCondition] = useState(rule.condition);
  const [actions, setActions] = useState<PolicyActionSpec[]>(
    normalizeRule(rule).actions ?? [{ type: "deliver" }]
  );

  function setAction(i: number, patch: Partial<PolicyActionSpec>) {
    setActions((a) => a.map((x, k) => (k === i ? { ...x, ...patch } : x)));
  }

  function submit() {
    if (!name.trim()) {
      toast.error("ルール名を入力してください");
      return;
    }
    if (actions.length === 0) {
      toast.error("アクションを 1 つ以上追加してください");
      return;
    }
    onSave({
      name: name.trim(),
      description: description.trim() || undefined,
      enabled: rule.enabled,
      priority: priority || undefined,
      tags: tags.trim()
        ? tags.split(",").map((t) => t.trim()).filter(Boolean)
        : undefined,
      condition: condition.trim(),
      actions,
    });
  }

  return (
    <Dialog open onClose={onClose}>
      <DialogHeader>
        <DialogTitle>{isNew ? "ルールを追加" : "ルールを編集"}</DialogTitle>
        <DialogDescription>
          条件がマッチしたときにアクションを適用します。
        </DialogDescription>
      </DialogHeader>

      <div className="space-y-3">
        <div className="grid grid-cols-2 gap-3">
          <label className="text-sm">
            <span className="text-gray-600">ルール名</span>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </label>
          <label className="text-sm">
            <span className="text-gray-600">優先度（小さいほど先）</span>
            <Input
              type="number"
              value={priority}
              onChange={(e) => setPriority(Number(e.target.value))}
            />
          </label>
        </div>

        <label className="block text-sm">
          <span className="text-gray-600">説明（任意）</span>
          <Input
            value={description}
            onChange={(e) => setDescription(e.target.value)}
          />
        </label>

        <label className="block text-sm">
          <span className="text-gray-600">タグ（カンマ区切り・任意）</span>
          <Input value={tags} onChange={(e) => setTags(e.target.value)} />
        </label>

        <label className="block text-sm">
          <span className="text-gray-600">条件式</span>
          <Input
            value={condition}
            onChange={(e) => setCondition(e.target.value)}
            placeholder='例: mail.direction == inbound && (a.detected == true || b.score >= 50)'
            className="font-mono"
          />
          <span className="mt-0.5 block text-xs text-gray-400">
            演算子: == != &gt;= &gt; &lt;= &lt; contains in_list &amp;&amp; || not ( )
          </span>
        </label>

        <div className="text-sm">
          <div className="mb-1 flex items-center justify-between">
            <span className="text-gray-600">アクション（上から順に適用）</span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setActions((a) => [...a, { type: "add_header" }])}
            >
              <Plus className="mr-1 h-3 w-3" />
              追加
            </Button>
          </div>
          <div className="space-y-2">
            {actions.map((a, i) => (
              <div key={i} className="flex flex-wrap items-center gap-2 border border-gray-200 p-2">
                <Select
                  value={a.type}
                  onChange={(e) => setAction(i, { type: e.target.value })}
                  className="w-44"
                >
                  <optgroup label="終端（評価を停止）">
                    {TERMINAL_ACTIONS.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </optgroup>
                  <optgroup label="非終端（次のルールへ続行）">
                    {NONTERMINAL_ACTIONS.map((t) => (
                      <option key={t} value={t}>
                        {t}
                      </option>
                    ))}
                  </optgroup>
                </Select>

                {a.type === "deliver" && (
                  <Input
                    placeholder="destination（deliverer 名 / host:port・任意）"
                    value={a.destination ?? ""}
                    onChange={(e) => setAction(i, { destination: e.target.value })}
                    className="flex-1 min-w-40"
                  />
                )}
                {a.type === "delay" && (
                  <Input
                    type="number"
                    placeholder="delay_minutes"
                    value={a.delay_minutes ?? ""}
                    onChange={(e) =>
                      setAction(i, { delay_minutes: Number(e.target.value) })
                    }
                    className="w-32"
                  />
                )}
                {a.type === "redirect" && (
                  <Input
                    placeholder="差し替え先アドレス（カンマ区切り可）"
                    value={a.value ?? ""}
                    onChange={(e) => setAction(i, { value: e.target.value })}
                    className="flex-1 min-w-40"
                  />
                )}
                {a.type === "strip_attachments" && (
                  <Input
                    placeholder="除去する拡張子（例: exe,zip・空なら全添付）"
                    value={a.value ?? ""}
                    onChange={(e) => setAction(i, { value: e.target.value })}
                    className="flex-1 min-w-40"
                  />
                )}
                {(a.type === "add_header" || a.type === "remove_header") && (
                  <Input
                    placeholder="ヘッダー名"
                    value={a.name ?? ""}
                    onChange={(e) => setAction(i, { name: e.target.value })}
                    className="w-40"
                  />
                )}
                {(a.type === "add_header" || a.type === "add_subject_prefix") && (
                  <Input
                    placeholder={a.type === "add_subject_prefix" ? "例: [EXTERNAL] " : "値"}
                    value={a.value ?? ""}
                    onChange={(e) => setAction(i, { value: e.target.value })}
                    className="flex-1 min-w-40"
                  />
                )}

                <Button
                  variant="ghost"
                  size="icon"
                  onClick={() => setActions((arr) => arr.filter((_, k) => k !== i))}
                  aria-label="削除"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </div>
            ))}
          </div>
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          キャンセル
        </Button>
        <Button onClick={submit}>OK</Button>
      </DialogFooter>
    </Dialog>
  );
}
