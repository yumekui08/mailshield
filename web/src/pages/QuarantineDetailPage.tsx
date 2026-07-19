import { useState } from "react";
import { useParams, useNavigate } from "react-router-dom";
import { toast } from "sonner";
import { ArrowLeft, Paperclip, Unlock, Trash2 } from "lucide-react";
import { useQuarantineDetail } from "../hooks/useQuarantine";
import { useRelease, useDelete } from "../hooks/useQuarantine";
import { ApiError } from "../lib/api";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
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
import { StatusBadge } from "../components/StatusBadge";
import { Badge } from "../components/ui/badge";
import { HelpButton } from "../help/HelpButton";
import { formatDate, formatBytes } from "../lib/utils";

type ActionType = "release" | "delete";

function AuthResultBadge({ result }: { result: string }) {
  const lower = result.toLowerCase();
  if (lower === "pass") return <Badge variant="green">pass</Badge>;
  if (lower === "fail") return <Badge variant="red">fail</Badge>;
  return <Badge variant="default">{result || "none"}</Badge>;
}

export function QuarantineDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [confirmAction, setConfirmAction] = useState<ActionType | null>(null);

  const { data, isLoading, error } = useQuarantineDetail(id ?? "");
  const release = useRelease();
  const deleteMsg = useDelete();

  function executeAction() {
    if (!confirmAction || !id) return;
    setConfirmAction(null);

    if (confirmAction === "release") {
      release.mutate(id, {
        onSuccess: () => {
          toast.success("隔離解放しました");
          navigate("/quarantine");
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 409) {
            toast.warning("変換後 EML の準備中です。しばらく待ってから再試行してください");
          } else {
            toast.error(`解放に失敗しました: ${err.message}`);
          }
        },
      });
    } else {
      deleteMsg.mutate(id, {
        onSuccess: () => {
          toast.success("削除しました");
          navigate("/quarantine");
        },
        onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
      });
    }
  }

  if (isLoading) {
    return (
      <div className="p-6 space-y-5">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="p-6">
        <button
          onClick={() => navigate("/quarantine")}
          className="flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900 mb-4"
        >
          <ArrowLeft className="h-4 w-4" />
          隔離メール一覧
        </button>
        <p className="text-red-600">メールが見つかりませんでした。</p>
      </div>
    );
  }

  const detectedCount = data.inspect_results.filter((r) => r.detected).length;

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center justify-between">
        <button
          onClick={() => navigate("/quarantine")}
          className="flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900"
        >
          <ArrowLeft className="h-4 w-4" />
          隔離メール一覧
        </button>
        <HelpButton helpKey="quarantineDetail" />
      </div>

      <div className="flex items-center justify-between flex-wrap gap-3">
        <div className="flex items-center gap-3 flex-wrap">
          <StatusBadge status={data.status} />
          <span className="text-sm text-gray-500">{formatDate(data.received_at)}</span>
          <span className="text-sm text-gray-500">{formatBytes(data.size_bytes)}</span>
          {data.has_attachment && (
            <span className="flex items-center gap-1 text-sm text-gray-500">
              <Paperclip className="h-4 w-4" />
              添付ファイルあり
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="success"
            onClick={() => setConfirmAction("release")}
            disabled={release.isPending || deleteMsg.isPending}
          >
            <Unlock className="h-4 w-4 mr-1.5" />
            解放
          </Button>
          <Button
            variant="destructive"
            onClick={() => setConfirmAction("delete")}
            disabled={release.isPending || deleteMsg.isPending}
          >
            <Trash2 className="h-4 w-4 mr-1.5" />
            削除
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>メール情報</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-xs font-medium text-gray-500">送信元</dt>
              <dd className="text-sm text-gray-900 mt-0.5">{data.from_address}</dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">宛先</dt>
              <dd className="text-sm text-gray-900 mt-0.5">
                {data.to_addresses.join(", ")}
              </dd>
            </div>
            <div className="sm:col-span-2">
              <dt className="text-xs font-medium text-gray-500">件名</dt>
              <dd className="text-sm text-gray-900 mt-0.5">{data.subject}</dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">SPF</dt>
              <dd className="mt-0.5">
                <AuthResultBadge result={data.spf_result} />
              </dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">DKIM</dt>
              <dd className="mt-0.5">
                <AuthResultBadge result={data.dkim_result} />
              </dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">DMARC</dt>
              <dd className="mt-0.5">
                <AuthResultBadge result={data.dmarc_result} />
              </dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">Rspamd スコア</dt>
              <dd className="text-sm text-gray-900 mt-0.5">{data.rspamd_score}</dd>
            </div>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <CardTitle>検査結果</CardTitle>
            {detectedCount > 0 && (
              <Badge variant="red">{detectedCount} 件検知</Badge>
            )}
          </div>
        </CardHeader>
        <CardContent className="p-0">
          {data.inspect_results.length === 0 ? (
            <p className="text-sm text-gray-500 px-6 pb-6">検査結果がありません</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>ワーカー名</TableHead>
                  <TableHead>スコア</TableHead>
                  <TableHead>検知</TableHead>
                  <TableHead>詳細</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.inspect_results.map((result) => (
                  <TableRow key={result.id}>
                    <TableCell className="font-mono text-xs">
                      {result.worker_name}
                    </TableCell>
                    <TableCell>
                      <span
                        className={
                          result.score >= 80
                            ? "text-red-600 font-medium"
                            : result.score >= 50
                            ? "text-yellow-600 font-medium"
                            : "text-gray-600"
                        }
                      >
                        {result.score}
                      </span>
                    </TableCell>
                    <TableCell>
                      {result.detected ? (
                        <Badge variant="red">検知</Badge>
                      ) : (
                        <Badge variant="green">正常</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      <pre className="text-xs text-gray-600 whitespace-pre-wrap max-w-xs">
                        {JSON.stringify(result.details, null, 2)}
                      </pre>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Dialog
        open={confirmAction !== null}
        onClose={() => setConfirmAction(null)}
      >
        <DialogHeader>
          <DialogTitle>
            {confirmAction === "release" ? "隔離を解放しますか？" : "メールを削除しますか？"}
          </DialogTitle>
          <DialogDescription>
            「{data.subject}」を
            {confirmAction === "release"
              ? "隔離から解放して配送します。"
              : "完全に削除します。この操作は取り消せません。"}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setConfirmAction(null)}>
            キャンセル
          </Button>
          <Button
            variant={confirmAction === "release" ? "success" : "destructive"}
            onClick={executeAction}
          >
            {confirmAction === "release" ? "解放する" : "削除する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
