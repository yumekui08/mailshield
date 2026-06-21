import { useNavigate } from "react-router-dom";
import { ClipboardCheck } from "lucide-react";
import { useApprovalList } from "../hooks/useApprovals";
import { Skeleton } from "../components/ui/skeleton";
import { Badge } from "../components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";
import { formatDate } from "../lib/utils";
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

function ApprovalStatusBadge({ status }: { status: ApprovalStatus }) {
  return <Badge variant={statusVariant[status]}>{statusLabel[status]}</Badge>;
}

export function ApprovalsPage() {
  const navigate = useNavigate();
  const { data, isLoading, isError } = useApprovalList();

  const items = data?.items ?? [];

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold text-gray-900">承認フロー</h1>
        {data && <Badge variant="blue">{items.length} 件</Badge>}
      </div>

      {isError && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          承認依頼一覧の取得に失敗しました。
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-white overflow-hidden">
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
                <TableHead>メール ID</TableHead>
                <TableHead>状態</TableHead>
                <TableHead>期限</TableHead>
                <TableHead>依頼日時</TableHead>
                <TableHead>決定日時</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={5}
                    className="text-center text-gray-500 py-10"
                  >
                    <div className="flex flex-col items-center gap-2">
                      <ClipboardCheck className="h-8 w-8 text-gray-300" />
                      承認依頼はありません
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
                    <TableCell className="text-sm font-mono text-gray-700">
                      {item.message_id.slice(0, 8)}…
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
                    <TableCell className="text-sm text-gray-500 whitespace-nowrap">
                      {item.decided_at ? formatDate(item.decided_at) : "—"}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </div>
    </div>
  );
}
