import { useState, useEffect } from "react";
import { toast } from "sonner";
import { Clock, Send, X, Paperclip } from "lucide-react";
import { useDelayedList, useCancelDelayed, useSendDelayedNow } from "../hooks/useDelayed";
import { usePagedList } from "../hooks/usePagedList";
import { Skeleton } from "../components/ui/skeleton";
import { Button } from "../components/ui/button";
import { PageHeader } from "../components/PageHeader";
import { Pagination } from "../components/Pagination";
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
import { formatDate } from "../lib/utils";
import type { DelayedRelease } from "../types";

// remainingLabel は release_at までの残り時間を「あとN分」のように返す。
function remainingLabel(releaseAt: string, now: number): string {
  const diffMs = new Date(releaseAt).getTime() - now;
  if (diffMs <= 0) return "まもなく送信";
  const min = Math.floor(diffMs / 60000);
  const sec = Math.floor((diffMs % 60000) / 1000);
  if (min >= 60) {
    const h = Math.floor(min / 60);
    return `あと約 ${h} 時間`;
  }
  if (min >= 1) return `あと ${min} 分`;
  return `あと ${sec} 秒`;
}

type ConfirmAction =
  | { type: "cancel"; item: DelayedRelease }
  | { type: "send"; item: DelayedRelease }
  | null;

export function DelayedPage() {
  const { data, isLoading, isError } = useDelayedList();
  const cancel = useCancelDelayed();
  const sendNow = useSendDelayedNow();
  const [confirm, setConfirm] = useState<ConfirmAction>(null);

  // 残り時間を毎秒更新するための now
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const items = data?.items ?? [];
  const { pageItems, meta, setPage } = usePagedList(data ? items : undefined, 20);
  const isPending = cancel.isPending || sendNow.isPending;

  function execute() {
    if (!confirm) return;
    const { type, item } = confirm;
    setConfirm(null);
    if (type === "cancel") {
      cancel.mutate(item.id, {
        onSuccess: () => toast.success("送信を取り消しました"),
        onError: (err) => toast.error(`取消に失敗しました: ${err.message}`),
      });
    } else {
      sendNow.mutate(item.id, {
        onSuccess: () => toast.success("送信しました"),
        onError: (err) => toast.error(`送信に失敗しました: ${err.message}`),
      });
    }
  }

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="送信待ち"
        description="送信ディレイにより保留中のメール。時間が来ると自動送信されます。取消・即時送信ができます"
        count={data ? items.length : undefined}
      />

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-800">
          送信待ち一覧の取得に失敗しました。
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-surface overflow-hidden">
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
                <TableHead>送信元</TableHead>
                <TableHead>宛先</TableHead>
                <TableHead>件名</TableHead>
                <TableHead>送信予定</TableHead>
                <TableHead>残り時間</TableHead>
                <TableHead>操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {pageItems.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <Clock className="h-8 w-8 text-gray-300" />
                      送信待ちのメールはありません
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                pageItems.map((item) => (
                  <TableRow key={item.id}>
                    <TableCell className="text-sm text-gray-700 max-w-[180px] truncate">
                      {item.from_address}
                    </TableCell>
                    <TableCell className="text-sm text-gray-600 max-w-[180px] truncate">
                      {item.to_addresses.join(", ")}
                    </TableCell>
                    <TableCell className="max-w-[220px]">
                      <div className="flex items-center gap-1.5 truncate">
                        {item.has_attachment && (
                          <Paperclip className="h-3.5 w-3.5 shrink-0 text-gray-400" />
                        )}
                        <span className="truncate text-gray-900">{item.subject}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-sm text-gray-500 whitespace-nowrap">
                      {formatDate(item.release_at)}
                    </TableCell>
                    <TableCell className="text-sm font-medium text-gray-700 whitespace-nowrap tabular-nums">
                      {remainingLabel(item.release_at, now)}
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setConfirm({ type: "send", item })}
                          disabled={isPending}
                        >
                          <Send className="h-3.5 w-3.5 mr-1" />
                          今すぐ送信
                        </Button>
                        <Button
                          variant="destructive"
                          size="sm"
                          onClick={() => setConfirm({ type: "cancel", item })}
                          disabled={isPending}
                        >
                          <X className="h-3.5 w-3.5 mr-1" />
                          取消
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

      <Dialog open={confirm !== null} onClose={() => setConfirm(null)}>
        <DialogHeader>
          <DialogTitle>
            {confirm?.type === "send" ? "今すぐ送信しますか？" : "送信を取り消しますか？"}
          </DialogTitle>
          <DialogDescription>
            {confirm?.type === "send"
              ? `「${confirm.item.subject}」を待機時間を待たずに送信します。`
              : confirm?.type === "cancel"
              ? `「${confirm.item.subject}」の送信を取り消します。この操作は取り消せません。`
              : ""}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setConfirm(null)}>
            キャンセル
          </Button>
          <Button
            variant={confirm?.type === "send" ? "default" : "destructive"}
            onClick={execute}
            disabled={isPending}
          >
            {confirm?.type === "send" ? "送信する" : "取り消す"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
