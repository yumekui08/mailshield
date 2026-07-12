import { useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { ArrowLeft, CheckCircle, XCircle, Paperclip } from "lucide-react";
import { useApprovalDetail, useApprove, useReject } from "../hooks/useApprovals";
import { ApiError } from "../lib/api";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";
import { Badge } from "../components/ui/badge";
import {
  Dialog,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "../components/ui/dialog";
import { formatDate, formatBytes } from "../lib/utils";
import type { ApprovalStatus } from "../types";

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

type ActionType = "approve" | "reject";

export function ApprovalDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [confirmAction, setConfirmAction] = useState<ActionType | null>(null);
  const [comment, setComment] = useState("");

  const { data, isLoading, error } = useApprovalDetail(id ?? "");
  const approve = useApprove();
  const reject = useReject();

  function executeAction() {
    if (!confirmAction || !id) return;
    const trimmedComment = comment.trim();

    const handlers = {
      onSuccess: () => {
        toast.success(
          confirmAction === "approve" ? "承認しました。メールを配送します。" : "却下しました。"
        );
        navigate("/approvals");
      },
      onError: (err: Error) => {
        if (err instanceof ApiError && err.status === 409) {
          toast.error("この承認依頼はすでに処理済みです");
        } else {
          toast.error(`操作に失敗しました: ${err.message}`);
        }
        setConfirmAction(null);
      },
    };

    if (confirmAction === "approve") {
      approve.mutate({ id, comment: trimmedComment || undefined }, handlers);
    } else {
      reject.mutate({ id, comment: trimmedComment || undefined }, handlers);
    }
  }

  function openConfirm(action: ActionType) {
    setComment("");
    setConfirmAction(action);
  }

  if (isLoading) {
    return (
      <div className="p-6 space-y-5">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="p-6">
        <button
          onClick={() => navigate("/approvals")}
          className="flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900 mb-4"
        >
          <ArrowLeft className="h-4 w-4" />
          承認フロー一覧
        </button>
        <p className="text-red-600">承認依頼が見つかりませんでした。</p>
      </div>
    );
  }

  const isPending = data.status === "pending";
  const isBusy = approve.isPending || reject.isPending;

  return (
    <div className="p-6 space-y-5">
      <button
        onClick={() => navigate("/approvals")}
        className="flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900"
      >
        <ArrowLeft className="h-4 w-4" />
        承認フロー一覧
      </button>

      {/* ヘッダー行 */}
      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3">
          <Badge variant={statusVariant[data.status]}>
            {statusLabel[data.status]}
          </Badge>
          <span className="text-sm text-gray-500">
            期限: {formatDate(data.expires_at)}
          </span>
          {data.decided_at && (
            <span className="text-sm text-gray-500">
              決定: {formatDate(data.decided_at)}
            </span>
          )}
        </div>

        {isPending && (
          <div className="flex items-center gap-2">
            <Button
              variant="success"
              onClick={() => openConfirm("approve")}
              disabled={isBusy}
            >
              <CheckCircle className="h-4 w-4 mr-1.5" />
              承認する
            </Button>
            <Button
              variant="destructive"
              onClick={() => openConfirm("reject")}
              disabled={isBusy}
            >
              <XCircle className="h-4 w-4 mr-1.5" />
              却下する
            </Button>
          </div>
        )}
      </div>

      {/* メール情報 */}
      <Card>
        <CardHeader>
          <CardTitle>メール情報</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div className="sm:col-span-2">
              <dt className="text-xs font-medium text-gray-500">件名</dt>
              <dd className="text-sm text-gray-900 mt-0.5 flex items-center gap-1.5">
                {data.subject}
                {data.has_attachment && (
                  <span className="flex items-center gap-1 text-gray-400">
                    <Paperclip className="h-3.5 w-3.5" />
                    <span className="text-xs">添付あり</span>
                  </span>
                )}
              </dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">送信元</dt>
              <dd className="text-sm text-gray-900 mt-0.5">{data.from_address}</dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">宛先</dt>
              <dd className="text-sm text-gray-900 mt-0.5">
                {data.to_addresses?.join(", ") ?? "—"}
              </dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">受信日時</dt>
              <dd className="text-sm text-gray-900 mt-0.5">
                {data.received_at ? formatDate(data.received_at) : "—"}
              </dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">サイズ</dt>
              <dd className="text-sm text-gray-900 mt-0.5">
                {data.size_bytes ? formatBytes(data.size_bytes) : "—"}
              </dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      {/* 承認情報 */}
      <Card>
        <CardHeader>
          <CardTitle>承認情報</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-xs font-medium text-gray-500">承認 ID</dt>
              <dd className="text-sm font-mono text-gray-700 mt-0.5">{data.id}</dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">状態</dt>
              <dd className="mt-0.5">
                <Badge variant={statusVariant[data.status]}>
                  {statusLabel[data.status]}
                </Badge>
              </dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">承認対象</dt>
              <dd className="text-sm text-gray-900 mt-0.5">
                {data.mailbox_emails && data.mailbox_emails.length > 0 ? (
                  <>
                    メールボックス承認{" "}
                    <span className="font-mono text-gray-700">
                      {data.mailbox_emails.join(", ")}
                    </span>
                    <span className="ml-1 text-xs text-gray-400">
                      （いずれかのメールボックスの admin が決定可）
                    </span>
                  </>
                ) : (
                  "個人承認（指定された承認者のみ決定可）"
                )}
              </dd>
            </div>
            {data.comment && (
              <div className="sm:col-span-2">
                <dt className="text-xs font-medium text-gray-500">コメント</dt>
                <dd className="text-sm text-gray-900 mt-0.5 whitespace-pre-wrap">
                  {data.comment}
                </dd>
              </div>
            )}
          </dl>
        </CardContent>
      </Card>

      {/* 承認/却下ダイアログ */}
      <Dialog open={confirmAction !== null} onClose={() => setConfirmAction(null)}>
        <DialogHeader>
          <DialogTitle>
            {confirmAction === "approve" ? "メールを承認しますか？" : "メールを却下しますか？"}
          </DialogTitle>
          <DialogDescription>
            {confirmAction === "approve"
              ? `「${data.subject}」を承認して配送します。`
              : `「${data.subject}」を却下します。`}
          </DialogDescription>
        </DialogHeader>

        <div className="px-6 pb-4 space-y-2">
          <label className="text-sm font-medium text-gray-700">
            コメント（任意）
          </label>
          <textarea
            className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm
                       placeholder:text-gray-400 focus:outline-none focus:ring-2
                       focus:ring-blue-500 focus:border-transparent resize-none"
            rows={3}
            placeholder="承認/却下の理由など"
            value={comment}
            onChange={(e) => setComment(e.target.value)}
          />
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={() => setConfirmAction(null)}>
            キャンセル
          </Button>
          <Button
            variant={confirmAction === "approve" ? "success" : "destructive"}
            onClick={executeAction}
            disabled={isBusy}
          >
            {isBusy
              ? "処理中..."
              : confirmAction === "approve"
              ? "承認する"
              : "却下する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
