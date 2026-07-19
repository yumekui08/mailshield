import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { ClipboardCheck, Search, Check, X } from "lucide-react";
import {
  useApprovalList,
  useBulkApprove,
  useBulkReject,
} from "../hooks/useApprovals";
import { Skeleton } from "../components/ui/skeleton";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Select } from "../components/ui/select";
import { PageHeader } from "../components/PageHeader";
import { Pagination } from "../components/Pagination";
import {
  Dialog,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "../components/ui/dialog";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";
import { formatDate } from "../lib/utils";
import type { ApprovalStatus, PageMeta } from "../types";

const PER_PAGE = 20;

const statusLabel: Record<ApprovalStatus, string> = {
  pending: "承認待ち",
  approved: "承認済み",
  rejected: "却下",
  expired: "期限切れ",
};

const statusVariant: Record<
  ApprovalStatus,
  "yellow" | "green" | "red" | "default"
> = {
  pending: "yellow",
  approved: "green",
  rejected: "red",
  expired: "default",
};

function ApprovalStatusBadge({ status }: { status: ApprovalStatus }) {
  return <Badge variant={statusVariant[status]}>{statusLabel[status]}</Badge>;
}

// 絞り込みプリセット → API に渡す status 配列（undefined はサーバ既定＝却下を除外）
type StatusPreset = "default" | "pending" | "approved" | "rejected" | "expired" | "all";
const statusPresetToParam: Record<StatusPreset, ApprovalStatus[] | undefined> = {
  default: undefined, // 却下以外（サーバ既定）
  pending: ["pending"],
  approved: ["approved"],
  rejected: ["rejected"],
  expired: ["expired"],
  all: ["pending", "approved", "rejected", "expired"],
};

type BulkAction = { type: "approve" | "reject"; ids: string[] } | null;

export function ApprovalsPage() {
  const navigate = useNavigate();

  const [search, setSearch] = useState("");
  const [debouncedQ, setDebouncedQ] = useState("");
  const [statusPreset, setStatusPreset] = useState<StatusPreset>("default");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkAction, setBulkAction] = useState<BulkAction>(null);
  const [bulkComment, setBulkComment] = useState("");

  useEffect(() => {
    const t = setTimeout(() => setDebouncedQ(search.trim()), 250);
    return () => clearTimeout(t);
  }, [search]);

  // 絞り込み変更で 1 ページ目に戻し、選択もクリアする
  useEffect(() => {
    setPage(1);
    setSelected(new Set());
  }, [debouncedQ, statusPreset]);

  const { data, isLoading, isError, isFetching } = useApprovalList({
    q: debouncedQ || undefined,
    status: statusPresetToParam[statusPreset],
    page,
    per_page: PER_PAGE,
  });

  const bulkApprove = useBulkApprove();
  const bulkReject = useBulkReject();
  const isBulkBusy = bulkApprove.isPending || bulkReject.isPending;

  const items = useMemo(() => data?.items ?? [], [data]);
  const total = data?.meta.total ?? 0;
  const meta: PageMeta = data?.meta ?? {
    total: 0,
    page,
    per_page: PER_PAGE,
    total_pages: 1,
  };

  // 決定できるのは pending のみ。選択対象は表示中ページの pending 行。
  const selectablePendingIds = useMemo(
    () => items.filter((it) => it.status === "pending").map((it) => it.id),
    [items]
  );
  const allPendingSelected =
    selectablePendingIds.length > 0 &&
    selectablePendingIds.every((id) => selected.has(id));

  function toggleOne(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAllOnPage() {
    setSelected((prev) => {
      const next = new Set(prev);
      if (allPendingSelected) {
        selectablePendingIds.forEach((id) => next.delete(id));
      } else {
        selectablePendingIds.forEach((id) => next.add(id));
      }
      return next;
    });
  }

  const selectedIds = Array.from(selected);

  function runBulk() {
    if (!bulkAction) return;
    const mutation = bulkAction.type === "approve" ? bulkApprove : bulkReject;
    const verb = bulkAction.type === "approve" ? "承認" : "却下";
    mutation.mutate(
      { ids: bulkAction.ids, comment: bulkComment.trim() || undefined },
      {
        onSuccess: (res) => {
          const okCount = res.succeeded.length;
          const ngCount = Object.keys(res.failed).length;
          if (okCount > 0) toast.success(`${okCount} 件を${verb}しました`);
          if (ngCount > 0)
            toast.warning(`${ngCount} 件は${verb}できませんでした（決定済み・権限等）`);
          setSelected(new Set());
          setBulkAction(null);
          setBulkComment("");
        },
        onError: (err) => toast.error(`一括${verb}に失敗しました: ${(err as Error).message}`),
      }
    );
  }

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="承認フロー"
        description="送信ポリシーにより承認者の判断を待っているメール"
        count={total}
      />

      {/* 検索・絞り込みバー */}
      <div className="flex flex-wrap items-center gap-2" data-help="approvals-filter">
        <div className="relative flex-1 min-w-56">
          <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 h-4 w-4 text-gray-400" />
          <Input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="件名・送信元・メール ID で検索"
            className="pl-8"
          />
        </div>
        <Select
          value={statusPreset}
          onChange={(e) => setStatusPreset(e.target.value as StatusPreset)}
          className="w-44"
          aria-label="ステータスで絞り込み"
        >
          <option value="default">却下以外（既定）</option>
          <option value="pending">承認待ち</option>
          <option value="approved">承認済み</option>
          <option value="rejected">却下のみ</option>
          <option value="expired">期限切れ</option>
          <option value="all">すべて</option>
        </Select>
        {isFetching && !isLoading && (
          <span className="text-xs text-gray-400">更新中…</span>
        )}
      </div>

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          承認依頼一覧の取得に失敗しました。
        </div>
      )}

      {/* 一括操作バー */}
      {selectedIds.length > 0 && (
        <div className="flex items-center gap-3 rounded-md border border-blue-200 bg-blue-50 px-4 py-2">
          <span className="text-sm text-blue-900">{selectedIds.length} 件選択中</span>
          <Button
            size="sm"
            variant="success"
            disabled={isBulkBusy}
            onClick={() => setBulkAction({ type: "approve", ids: selectedIds })}
          >
            <Check className="h-3.5 w-3.5 mr-1" />
            一括承認
          </Button>
          <Button
            size="sm"
            variant="destructive"
            disabled={isBulkBusy}
            onClick={() => setBulkAction({ type: "reject", ids: selectedIds })}
          >
            <X className="h-3.5 w-3.5 mr-1" />
            一括却下
          </Button>
          <button
            className="text-xs text-gray-500 hover:text-gray-800"
            onClick={() => setSelected(new Set())}
          >
            選択解除
          </button>
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-surface overflow-hidden" data-help="approvals-table">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {Array.from({ length: 5 }).map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-10">
                  <input
                    type="checkbox"
                    aria-label="ページ内の承認待ちを全選択"
                    checked={allPendingSelected}
                    disabled={selectablePendingIds.length === 0}
                    onChange={toggleAllOnPage}
                  />
                </TableHead>
                <TableHead>件名</TableHead>
                <TableHead>送信元</TableHead>
                <TableHead>承認対象</TableHead>
                <TableHead>状態</TableHead>
                <TableHead>期限</TableHead>
                <TableHead>依頼日時</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <ClipboardCheck className="h-8 w-8 text-gray-300" />
                      {debouncedQ || statusPreset !== "default"
                        ? "条件に一致する承認依頼はありません"
                        : "承認依頼はありません"}
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                items.map((item) => (
                  <TableRow
                    key={item.id}
                    className="cursor-pointer hover:bg-gray-50"
                    onClick={() => navigate(`/approvals/${item.id}`)}
                  >
                    <TableCell onClick={(e) => e.stopPropagation()}>
                      {item.status === "pending" ? (
                        <input
                          type="checkbox"
                          aria-label="この依頼を選択"
                          checked={selected.has(item.id)}
                          onChange={() => toggleOne(item.id)}
                        />
                      ) : null}
                    </TableCell>
                    <TableCell className="text-sm text-gray-800 max-w-64 truncate">
                      {item.subject || <span className="text-gray-400">（件名なし）</span>}
                    </TableCell>
                    <TableCell className="text-sm text-gray-600 max-w-48 truncate">
                      {item.from_address}
                    </TableCell>
                    <TableCell className="text-sm text-gray-700">
                      {item.mailbox_emails && item.mailbox_emails.length > 0 ? (
                        item.mailbox_emails.join(", ")
                      ) : (
                        <span className="text-gray-400">個人承認</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <ApprovalStatusBadge status={item.status} />
                    </TableCell>
                    <TableCell className="text-sm text-gray-500 whitespace-nowrap">
                      {formatDate(item.expires_at)}
                    </TableCell>
                    <TableCell className="text-sm text-gray-500 whitespace-nowrap">
                      {formatDate(item.created_at)}
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

      {/* 一括承認/却下の確認ダイアログ */}
      <Dialog open={bulkAction !== null} onClose={() => setBulkAction(null)}>
        <DialogHeader>
          <DialogTitle>
            {bulkAction?.type === "approve"
              ? `${bulkAction?.ids.length} 件を一括承認しますか？`
              : `${bulkAction?.ids.length} 件を一括却下しますか？`}
          </DialogTitle>
          <DialogDescription>
            {bulkAction?.type === "approve"
              ? "選択したメールを配送します。すでに決定済みのものはスキップされます。"
              : "選択したメールを却下します。内部送信者には却下通知が送られます。"}
          </DialogDescription>
        </DialogHeader>
        <div className="px-5 py-4 space-y-2">
          <label className="text-sm font-medium text-gray-700">コメント（任意・全件共通）</label>
          <Input
            value={bulkComment}
            onChange={(e) => setBulkComment(e.target.value)}
            placeholder="判断理由など"
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setBulkAction(null)}>
            キャンセル
          </Button>
          <Button
            variant={bulkAction?.type === "approve" ? "success" : "destructive"}
            onClick={runBulk}
            disabled={isBulkBusy}
          >
            {isBulkBusy
              ? "処理中…"
              : bulkAction?.type === "approve"
                ? "一括承認する"
                : "一括却下する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
