import { useEffect, useMemo, useRef, useState } from "react";
import { Search, Check, X } from "lucide-react";
import { searchUsers } from "../lib/api";
import type { UserRecord } from "../types";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import {
  Dialog,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "./ui/dialog";

interface UserPickerProps {
  open: boolean;
  title?: string;
  description?: string;
  /** 既に選択不可（割り当て済み等）にするユーザー ID */
  excludeIds?: Set<string>;
  onClose: () => void;
  /** 決定時に選択したユーザーを返す */
  onConfirm: (users: UserRecord[]) => void;
}

// UserPicker はサーバサイド検索で複数ユーザーを選択するモーダル。
// 数千ユーザー環境でも破綻しないよう、プルダウンではなく検索 + ページング取得を使う。
export function UserPicker({
  open,
  title = "ユーザーを選択",
  description,
  excludeIds,
  onClose,
  onConfirm,
}: UserPickerProps) {
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [results, setResults] = useState<UserRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [total, setTotal] = useState(0);
  // 選択中ユーザー（ID → レコード）。検索語を変えても保持する。
  const [selected, setSelected] = useState<Map<string, UserRecord>>(new Map());
  const reqSeq = useRef(0);

  // モーダルを開くたびに状態リセット
  useEffect(() => {
    if (open) {
      setQuery("");
      setDebounced("");
      setSelected(new Map());
      setError(null);
    }
  }, [open]);

  // 入力のデバウンス（250ms）
  useEffect(() => {
    const t = setTimeout(() => setDebounced(query.trim()), 250);
    return () => clearTimeout(t);
  }, [query]);

  // 検索実行
  useEffect(() => {
    if (!open) return;
    const seq = ++reqSeq.current;
    setLoading(true);
    setError(null);
    searchUsers(debounced, 50)
      .then((res) => {
        if (seq !== reqSeq.current) return; // 古いレスポンスは破棄
        setResults(res.data);
        setTotal(res.meta.total);
      })
      .catch(() => {
        if (seq !== reqSeq.current) return;
        setError("ユーザー検索に失敗しました");
      })
      .finally(() => {
        if (seq === reqSeq.current) setLoading(false);
      });
  }, [debounced, open]);

  function toggle(u: UserRecord) {
    setSelected((prev) => {
      const next = new Map(prev);
      if (next.has(u.id)) next.delete(u.id);
      else next.set(u.id, u);
      return next;
    });
  }

  const selectedList = useMemo(() => [...selected.values()], [selected]);

  return (
    <Dialog open={open} onClose={onClose}>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        {description && <DialogDescription>{description}</DialogDescription>}
      </DialogHeader>

      <div className="space-y-3">
        <div className="relative">
          <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-gray-400" />
          <Input
            autoFocus
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="メールアドレス・表示名で検索…"
            className="pl-8"
          />
        </div>

        {/* 選択中チップ */}
        {selectedList.length > 0 && (
          <div className="flex flex-wrap gap-1">
            {selectedList.map((u) => (
              <span
                key={u.id}
                className="inline-flex items-center gap-1 border border-blue-300 bg-blue-50 px-1.5 py-0.5 text-xs text-blue-800"
              >
                {u.display_name || u.email}
                <button onClick={() => toggle(u)} aria-label="選択解除">
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}
          </div>
        )}

        {/* 結果一覧 */}
        <div className="max-h-72 overflow-y-auto border border-gray-200">
          {loading && (
            <div className="px-3 py-4 text-sm text-gray-400">検索中…</div>
          )}
          {error && <div className="px-3 py-4 text-sm text-red-600">{error}</div>}
          {!loading && !error && results.length === 0 && (
            <div className="px-3 py-4 text-sm text-gray-400">
              該当するユーザーがありません
            </div>
          )}
          {results.map((u) => {
            const isExcluded = excludeIds?.has(u.id) ?? false;
            const isSelected = selected.has(u.id);
            return (
              <button
                key={u.id}
                disabled={isExcluded}
                onClick={() => toggle(u)}
                className={`flex w-full items-center justify-between px-3 py-2 text-left text-sm ${
                  isExcluded
                    ? "cursor-not-allowed opacity-40"
                    : "hover:bg-gray-50"
                } ${isSelected ? "bg-blue-50" : ""}`}
              >
                <div className="min-w-0">
                  <div className="font-medium truncate">
                    {u.display_name || u.email}
                  </div>
                  <div className="text-xs text-gray-400 truncate">{u.email}</div>
                </div>
                {isExcluded ? (
                  <span className="text-xs text-gray-400 shrink-0">割り当て済み</span>
                ) : isSelected ? (
                  <Check className="h-4 w-4 text-blue-600 shrink-0" />
                ) : null}
              </button>
            );
          })}
        </div>
        <div className="text-xs text-gray-400">
          {total >= 50
            ? "上位 50 件を表示。絞り込むには検索してください。"
            : `${total} 件`}
          {selectedList.length > 0 && ` / ${selectedList.length} 名選択中`}
        </div>
      </div>

      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          キャンセル
        </Button>
        <Button
          onClick={() => onConfirm(selectedList)}
          disabled={selectedList.length === 0}
        >
          {selectedList.length > 0
            ? `${selectedList.length} 名を追加`
            : "追加"}
        </Button>
      </DialogFooter>
    </Dialog>
  );
}
