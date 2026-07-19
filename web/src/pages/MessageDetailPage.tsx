import { useParams, useNavigate, Link } from "react-router-dom";
import { ArrowLeft, Paperclip, ExternalLink, Download, File } from "lucide-react";
import { useState, useEffect } from "react";
import { useMessageDetail } from "../hooks/useMessages";
import { getMessageEMLURL, getAttachmentsByMessage, getAttachmentDownloadURL, type Attachment } from "../lib/api";
import { Card, CardContent, CardHeader, CardTitle } from "../components/ui/card";
import { Skeleton } from "../components/ui/skeleton";
import { StatusBadge } from "../components/StatusBadge";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";
import { HelpButton } from "../help/HelpButton";
import { formatDate, formatBytes } from "../lib/utils";

function AuthResultBadge({ result }: { result: string }) {
  const lower = result?.toLowerCase();
  if (lower === "pass") return <Badge variant="green">pass</Badge>;
  if (lower === "fail") return <Badge variant="red">fail</Badge>;
  return <Badge variant="default">{result || "none"}</Badge>;
}

export function MessageDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const { data, isLoading, error } = useMessageDetail(id ?? "");
  const [isDownloading, setIsDownloading] = useState(false);
  const [attachments, setAttachments] = useState<Attachment[]>([]);

  useEffect(() => {
    if (!id) return;
    getAttachmentsByMessage(id).then(setAttachments).catch(() => {});
  }, [id]);

  async function handleEMLDownload() {
    if (!id) return;
    setIsDownloading(true);
    try {
      const { url } = await getMessageEMLURL(id);
      const a = document.createElement("a");
      a.href = url;
      a.download = `${id}.eml`;
      a.click();
    } finally {
      setIsDownloading(false);
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
          onClick={() => navigate("/messages")}
          className="flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900 mb-4"
        >
          <ArrowLeft className="h-4 w-4" />
          メール処理ログ
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
          onClick={() => navigate("/messages")}
          className="flex items-center gap-1 text-sm text-gray-600 hover:text-gray-900"
        >
          <ArrowLeft className="h-4 w-4" />
          メール処理ログ
        </button>
        <HelpButton helpKey="messageDetail" />
      </div>

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
        {data.status === "quarantined" && (
          <Link
            to={`/quarantine/${data.id}`}
            className="flex items-center gap-1 text-sm text-blue-600 hover:text-blue-800 ml-auto"
          >
            <ExternalLink className="h-3.5 w-3.5" />
            隔離管理で開く
          </Link>
        )}
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>メール情報</CardTitle>
            <Button
              variant="outline"
              size="sm"
              onClick={handleEMLDownload}
              disabled={isDownloading}
            >
              <Download className="h-4 w-4 mr-1.5" />
              {isDownloading ? "取得中..." : "EML ダウンロード"}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <div>
              <dt className="text-xs font-medium text-gray-500">送信元</dt>
              <dd className="text-sm text-gray-900 mt-0.5">{data.from_address}</dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">宛先</dt>
              <dd className="text-sm text-gray-900 mt-0.5">{data.to_addresses.join(", ")}</dd>
            </div>
            <div className="sm:col-span-2">
              <dt className="text-xs font-medium text-gray-500">件名</dt>
              <dd className="text-sm text-gray-900 mt-0.5">{data.subject}</dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">SPF</dt>
              <dd className="mt-0.5"><AuthResultBadge result={data.spf_result} /></dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">DKIM</dt>
              <dd className="mt-0.5"><AuthResultBadge result={data.dkim_result} /></dd>
            </div>
            <div>
              <dt className="text-xs font-medium text-gray-500">DMARC</dt>
              <dd className="mt-0.5"><AuthResultBadge result={data.dmarc_result} /></dd>
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
                    <TableCell className="font-mono text-xs">{result.worker_name}</TableCell>
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

      {attachments.length > 0 && (
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <CardTitle>分離済み添付ファイル</CardTitle>
              <Badge variant="default">{attachments.length} 件</Badge>
            </div>
          </CardHeader>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead>ファイル名</TableHead>
                  <TableHead>サイズ</TableHead>
                  <TableHead>種別</TableHead>
                  <TableHead>状態</TableHead>
                  <TableHead></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {attachments.map((att) => (
                  <TableRow key={att.id}>
                    <TableCell>
                      <span className="flex items-center gap-1.5">
                        <File className="h-4 w-4 text-gray-400 shrink-0" />
                        <span className="font-mono text-xs">{att.filename}</span>
                      </span>
                    </TableCell>
                    <TableCell className="text-sm text-gray-600">
                      {formatBytes(att.size_bytes)}
                    </TableCell>
                    <TableCell className="text-xs text-gray-500">{att.content_type}</TableCell>
                    <TableCell>
                      {att.is_disabled ? (
                        <Badge variant="red">無効</Badge>
                      ) : (
                        <Badge variant="green">有効</Badge>
                      )}
                    </TableCell>
                    <TableCell>
                      {!att.is_disabled && (
                        <a
                          href={getAttachmentDownloadURL(att.download_token, att.filename)}
                          download={att.filename}
                          className="inline-flex items-center gap-1 text-xs text-blue-600 hover:text-blue-800 border border-gray-200 rounded px-2 py-1"
                        >
                          <Download className="h-3.5 w-3.5" />
                          DL
                        </a>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
