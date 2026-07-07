import { useState } from "react";
import { ClipboardList } from "lucide-react";
import { useAuditLogs } from "../hooks/useAuditLogs";
import { useMe } from "../hooks/useAuth";
import { Input } from "../components/ui/input";
import { Button } from "../components/ui/button";
import { Badge } from "../components/ui/badge";
import { Skeleton } from "../components/ui/skeleton";
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
import type { AuditLogParams } from "../types";

function formatDate(iso: string): string {
  return new Date(iso).toLocaleString("ja-JP", { timeZone: "Asia/Tokyo" });
}

function eventTypeBadge(eventType: string) {
  if (eventType.startsWith("auth.")) return <Badge variant="blue">{eventType}</Badge>;
  if (eventType.startsWith("quarantine.")) return <Badge variant="green">{eventType}</Badge>;
  if (eventType.startsWith("user.")) return <Badge variant="default">{eventType}</Badge>;
  if (eventType.startsWith("mailbox.")) return <Badge>{eventType}</Badge>;
  return <Badge variant="default">{eventType}</Badge>;
}

export function AuditLogsPage() {
  const { data: me } = useMe();

  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<Omit<AuditLogParams, "page" | "per_page">>({});
  const [draft, setDraft] = useState({
    event_type: "",
    actor_id: "",
    from_date: "",
    to_date: "",
  });

  const params: AuditLogParams = { page, per_page: 50, ...filters };
  const { data, isLoading, isError } = useAuditLogs(params);

  if (me && me.role !== "admin") {
    return (
      <div className="p-6">
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          この画面は管理者（admin）のみアクセスできます。
        </div>
      </div>
    );
  }

  function applyFilters() {
    setPage(1);
    setFilters({
      event_type: draft.event_type || undefined,
      actor_id: draft.actor_id || undefined,
      from_date: draft.from_date || undefined,
      to_date: draft.to_date || undefined,
    });
  }

  function clearFilters() {
    setPage(1);
    setDraft({ event_type: "", actor_id: "", from_date: "", to_date: "" });
    setFilters({});
  }

  const logs = data?.data ?? [];

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="監査ログ"
        description="ログイン・隔離操作・設定変更などの操作履歴"
        count={data?.meta.total}
      />

      {/* フィルターバー */}
      <div className="rounded-lg border border-gray-200 bg-surface p-4 space-y-3">
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          <div className="space-y-1">
            <label className="text-xs font-medium text-gray-600">イベントタイプ（前方一致）</label>
            <Input
              placeholder="例: auth. / quarantine."
              value={draft.event_type}
              onChange={(e) => setDraft((d) => ({ ...d, event_type: e.target.value }))}
              onKeyDown={(e) => e.key === "Enter" && applyFilters()}
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium text-gray-600">操作者ID</label>
            <Input
              placeholder="ユーザーUUID"
              value={draft.actor_id}
              onChange={(e) => setDraft((d) => ({ ...d, actor_id: e.target.value }))}
              onKeyDown={(e) => e.key === "Enter" && applyFilters()}
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium text-gray-600">開始日時</label>
            <Input
              type="datetime-local"
              value={draft.from_date}
              onChange={(e) => setDraft((d) => ({ ...d, from_date: e.target.value }))}
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs font-medium text-gray-600">終了日時</label>
            <Input
              type="datetime-local"
              value={draft.to_date}
              onChange={(e) => setDraft((d) => ({ ...d, to_date: e.target.value }))}
            />
          </div>
        </div>
        <div className="flex gap-2">
          <Button size="sm" onClick={applyFilters}>絞り込む</Button>
          <Button size="sm" variant="outline" onClick={clearFilters}>クリア</Button>
        </div>
      </div>

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          監査ログの取得に失敗しました。
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
                <TableHead className="w-[200px]">日時</TableHead>
                <TableHead>イベントタイプ</TableHead>
                <TableHead>操作者</TableHead>
                <TableHead>対象</TableHead>
                <TableHead>IPアドレス</TableHead>
                <TableHead>詳細</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {logs.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="text-center text-gray-500 py-10">
                    <div className="flex flex-col items-center gap-2">
                      <ClipboardList className="h-8 w-8 text-gray-300" />
                      ログがありません
                    </div>
                  </TableCell>
                </TableRow>
              ) : (
                logs.map((log) => (
                  <TableRow key={log.id}>
                    <TableCell className="text-xs text-gray-500 whitespace-nowrap">
                      {formatDate(log.created_at)}
                    </TableCell>
                    <TableCell>{eventTypeBadge(log.event_type)}</TableCell>
                    <TableCell className="text-sm">
                      {log.actor_email ?? (log.actor_id ? (
                        <span className="text-xs text-gray-400">{log.actor_id}</span>
                      ) : (
                        <span className="text-xs text-gray-300">—</span>
                      ))}
                    </TableCell>
                    <TableCell className="text-sm">
                      {log.target_type && (
                        <span className="text-xs text-gray-500">
                          {log.target_type}
                          {log.target_id && ` / ${log.target_id}`}
                        </span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-gray-500">
                      {log.ip_address ?? "—"}
                    </TableCell>
                    <TableCell className="text-xs text-gray-500 max-w-[240px] truncate">
                      {log.detail ? JSON.stringify(log.detail) : "—"}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
        {data && (
          <Pagination
            meta={data.meta}
            onPageChange={(p) => setPage(p)}
            className="border-t border-gray-200 bg-gray-50 px-3 py-2"
          />
        )}
      </div>
    </div>
  );
}
