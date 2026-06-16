import { useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  createColumnHelper,
} from "@tanstack/react-table";
import { Paperclip } from "lucide-react";
import { useMessageList } from "../hooks/useMessages";
import { Input } from "../components/ui/input";
import { Select } from "../components/ui/select";
import { Skeleton } from "../components/ui/skeleton";
import { StatusBadge } from "../components/StatusBadge";
import { Badge } from "../components/ui/badge";
import { Pagination } from "../components/Pagination";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../components/ui/table";
import { formatDate, formatBytes } from "../lib/utils";
import type { Message } from "../types";

const columnHelper = createColumnHelper<Message>();

const STATUS_OPTIONS = [
  { value: "", label: "すべてのステータス" },
  { value: "delivered", label: "配送済み" },
  { value: "quarantined", label: "隔離中" },
  { value: "rejected", label: "拒否" },
  { value: "received", label: "受信済み" },
  { value: "processing", label: "処理中" },
];

export function MessagesPage() {
  const navigate = useNavigate();
  const [page, setPage] = useState(1);
  const [fromFilter, setFromFilter] = useState("");
  const [subjectFilter, setSubjectFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");
  const [attachmentFilter, setAttachmentFilter] = useState<boolean | "">("");

  const { data, isLoading, isError } = useMessageList({
    page,
    per_page: 20,
    from: fromFilter || undefined,
    subject: subjectFilter || undefined,
    status: statusFilter || undefined,
    has_attachment: attachmentFilter,
  });

  const columns = [
    columnHelper.accessor("received_at", {
      header: "受信日時",
      cell: (info) => (
        <span className="text-xs text-gray-500 whitespace-nowrap">
          {formatDate(info.getValue())}
        </span>
      ),
    }),
    columnHelper.accessor("from_address", {
      header: "送信元",
      cell: (info) => (
        <span className="text-sm truncate max-w-[180px] block">{info.getValue()}</span>
      ),
    }),
    columnHelper.accessor("to_addresses", {
      header: "宛先",
      cell: (info) => (
        <span className="text-sm truncate max-w-[180px] block text-gray-600">
          {info.getValue().join(", ")}
        </span>
      ),
    }),
    columnHelper.accessor("subject", {
      header: "件名",
      cell: (info) => (
        <div className="flex items-center gap-1.5 max-w-[240px]">
          {info.row.original.has_attachment && (
            <Paperclip className="h-3.5 w-3.5 text-gray-400 shrink-0" />
          )}
          <span className="text-sm truncate">{info.getValue()}</span>
        </div>
      ),
    }),
    columnHelper.accessor("status", {
      header: "ステータス",
      cell: (info) => <StatusBadge status={info.getValue()} />,
    }),
    columnHelper.accessor("size_bytes", {
      header: "サイズ",
      cell: (info) => (
        <span className="text-xs text-gray-500 whitespace-nowrap">
          {formatBytes(info.getValue())}
        </span>
      ),
    }),
  ];

  const table = useReactTable({
    data: data?.data ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
  });

  return (
    <div className="p-6 space-y-5">
      <div className="flex items-center gap-3">
        <h1 className="text-xl font-semibold text-gray-900">メール処理ログ</h1>
        {data && <Badge variant="blue">{data.meta.total} 件</Badge>}
      </div>

      <div className="flex items-center gap-3 flex-wrap">
        <Input
          placeholder="送信元で絞り込み"
          value={fromFilter}
          onChange={(e) => { setFromFilter(e.target.value); setPage(1); }}
          className="w-52"
        />
        <Input
          placeholder="件名で絞り込み"
          value={subjectFilter}
          onChange={(e) => { setSubjectFilter(e.target.value); setPage(1); }}
          className="w-52"
        />
        <Select
          value={statusFilter}
          onChange={(e) => { setStatusFilter(e.target.value); setPage(1); }}
          className="w-44"
        >
          {STATUS_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>{o.label}</option>
          ))}
        </Select>
        <Select
          value={attachmentFilter === "" ? "" : String(attachmentFilter)}
          onChange={(e) => {
            const v = e.target.value;
            setAttachmentFilter(v === "" ? "" : v === "true");
            setPage(1);
          }}
          className="w-40"
        >
          <option value="">添付ファイル: 全て</option>
          <option value="true">添付あり</option>
          <option value="false">添付なし</option>
        </Select>
      </div>

      {isError && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          メール一覧の取得に失敗しました。
        </div>
      )}

      <div className="rounded-lg border border-gray-200 bg-white overflow-hidden">
        {isLoading ? (
          <div className="p-4 space-y-3">
            {[...Array(5)].map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
          </div>
        ) : (
          <Table>
            <TableHeader>
              {table.getHeaderGroups().map((hg) => (
                <TableRow key={hg.id} className="hover:bg-transparent">
                  {hg.headers.map((h) => (
                    <TableHead key={h.id}>
                      {flexRender(h.column.columnDef.header, h.getContext())}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={columns.length} className="text-center text-gray-500 py-10">
                    メールがありません
                  </TableCell>
                </TableRow>
              ) : (
                table.getRowModel().rows.map((row) => (
                  <TableRow
                    key={row.id}
                    className="cursor-pointer"
                    onClick={() => navigate(`/messages/${row.original.id}`)}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
      </div>

      {data && data.meta.total_pages > 1 && (
        <Pagination meta={data.meta} onPageChange={setPage} />
      )}
    </div>
  );
}
