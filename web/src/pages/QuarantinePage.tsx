import { useState } from "react";
import { useNavigate } from "react-router-dom";
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  createColumnHelper,
  type RowSelectionState,
} from "@tanstack/react-table";
import { toast } from "sonner";
import { Paperclip, Trash2, Unlock } from "lucide-react";
import { useQuarantineList, useRelease, useDelete, useBulkRelease, useBulkDelete } from "../hooks/useQuarantine";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Select } from "../components/ui/select";
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
import { PageHeader } from "../components/PageHeader";
import { Pagination } from "../components/Pagination";
import { formatDate, formatBytes } from "../lib/utils";
import { ApiError } from "../lib/api";
import type { Message } from "../types";

const columnHelper = createColumnHelper<Message>();

type ConfirmAction =
  | { type: "release"; id: string; subject: string }
  | { type: "delete"; id: string; subject: string }
  | { type: "bulk-release"; ids: string[] }
  | { type: "bulk-delete"; ids: string[] };

export function QuarantinePage() {
  const navigate = useNavigate();
  const [page, setPage] = useState(1);
  const [fromFilter, setFromFilter] = useState("");
  const [subjectFilter, setSubjectFilter] = useState("");
  const [attachmentFilter, setAttachmentFilter] = useState<boolean | "">("");
  const [confirmAction, setConfirmAction] = useState<ConfirmAction | null>(null);
  const [rowSelection, setRowSelection] = useState<RowSelectionState>({});

  const { data, isLoading, isError, error } = useQuarantineList({
    page,
    per_page: 20,
    from: fromFilter || undefined,
    subject: subjectFilter || undefined,
    has_attachment: attachmentFilter,
  });

  const release = useRelease();
  const deleteMsg = useDelete();
  const bulkRelease = useBulkRelease();
  const bulkDelete = useBulkDelete();

  const isPending =
    release.isPending || deleteMsg.isPending ||
    bulkRelease.isPending || bulkDelete.isPending;

  function handleRelease(e: React.MouseEvent, id: string, subject: string) {
    e.stopPropagation();
    setConfirmAction({ type: "release", id, subject });
  }

  function handleDelete(e: React.MouseEvent, id: string, subject: string) {
    e.stopPropagation();
    setConfirmAction({ type: "delete", id, subject });
  }

  function executeAction() {
    if (!confirmAction) return;
    // setState の前にキャプチャして型の絞り込みを維持する
    const action = confirmAction;
    setConfirmAction(null);

    if (action.type === "release") {
      release.mutate(action.id, {
        onSuccess: () => toast.success("隔離解放しました"),
        onError: (err) => toast.error(`解放に失敗しました: ${err.message}`),
      });
    } else if (action.type === "delete") {
      deleteMsg.mutate(action.id, {
        onSuccess: () => toast.success("削除しました"),
        onError: (err) => toast.error(`削除に失敗しました: ${err.message}`),
      });
    } else if (action.type === "bulk-release") {
      bulkRelease.mutate(action.ids, {
        onSuccess: (result) => {
          setRowSelection({});
          if (result.failed.length === 0) {
            toast.success(`${result.succeeded.length} 件を解放しました`);
          } else {
            toast.warning(
              `${result.succeeded.length} 件解放・${result.failed.length} 件失敗`
            );
          }
        },
        onError: (err) => toast.error(`一括解放に失敗しました: ${err.message}`),
      });
    } else {
      bulkDelete.mutate(action.ids, {
        onSuccess: (result) => {
          setRowSelection({});
          toast.success(`${result.succeeded.length} 件を削除しました`);
        },
        onError: (err) => toast.error(`一括削除に失敗しました: ${err.message}`),
      });
    }
  }

  const columns = [
    columnHelper.display({
      id: "select",
      header: ({ table }) => (
        <input
          type="checkbox"
          className="h-4 w-4 rounded border-gray-300 accent-blue-600 cursor-pointer"
          checked={table.getIsAllPageRowsSelected()}
          ref={(el) => {
            if (el) el.indeterminate = table.getIsSomePageRowsSelected();
          }}
          onChange={table.getToggleAllPageRowsSelectedHandler()}
          onClick={(e) => e.stopPropagation()}
        />
      ),
      cell: ({ row }) => (
        <input
          type="checkbox"
          className="h-4 w-4 rounded border-gray-300 accent-blue-600 cursor-pointer"
          checked={row.getIsSelected()}
          onChange={row.getToggleSelectedHandler()}
          onClick={(e) => e.stopPropagation()}
        />
      ),
    }),
    columnHelper.accessor("received_at", {
      header: "受信日時",
      cell: (info) => (
        <span className="text-xs text-gray-600 whitespace-nowrap">
          {formatDate(info.getValue())}
        </span>
      ),
    }),
    columnHelper.accessor("from_address", {
      header: "送信元",
      cell: (info) => (
        <span className="text-sm truncate max-w-xs block">{info.getValue()}</span>
      ),
    }),
    columnHelper.accessor("subject", {
      header: "件名",
      cell: (info) => (
        <span className="text-sm truncate max-w-xs block">{info.getValue()}</span>
      ),
    }),
    columnHelper.accessor("size_bytes", {
      header: "サイズ",
      cell: (info) => (
        <span className="text-xs text-gray-600 whitespace-nowrap">
          {formatBytes(info.getValue())}
        </span>
      ),
    }),
    columnHelper.accessor("has_attachment", {
      header: "添付",
      cell: (info) =>
        info.getValue() ? (
          <Paperclip className="h-4 w-4 text-gray-500" />
        ) : null,
    }),
    columnHelper.display({
      id: "actions",
      header: "アクション",
      cell: ({ row }) => (
        <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
          <Button
            variant="success"
            size="sm"
            onClick={(e) =>
              handleRelease(e, row.original.id, row.original.subject)
            }
            disabled={isPending}
          >
            <Unlock className="h-3.5 w-3.5 mr-1" />
            解放
          </Button>
          <Button
            variant="destructive"
            size="sm"
            onClick={(e) =>
              handleDelete(e, row.original.id, row.original.subject)
            }
            disabled={isPending}
          >
            <Trash2 className="h-3.5 w-3.5 mr-1" />
            削除
          </Button>
        </div>
      ),
    }),
  ];

  const table = useReactTable({
    data: data?.data ?? [],
    columns,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    enableRowSelection: true,
    onRowSelectionChange: setRowSelection,
    getRowId: (row) => row.id,
    state: { rowSelection },
  });

  const selectedIds = table.getSelectedRowModel().rows.map((r) => r.original.id);
  const selectedCount = selectedIds.length;

  return (
    <div className="p-6 space-y-4">
      <PageHeader
        title="隔離メール"
        description="ポリシーにより配送を保留しているメール。解放すると配送されます"
        count={data?.meta.total}
      />

      <div className="flex items-center gap-2 flex-wrap">
        <Input
          placeholder="送信元で絞り込み"
          value={fromFilter}
          onChange={(e) => {
            setFromFilter(e.target.value);
            setPage(1);
            setRowSelection({});
          }}
          className="w-52"
        />
        <Input
          placeholder="件名で絞り込み"
          value={subjectFilter}
          onChange={(e) => {
            setSubjectFilter(e.target.value);
            setPage(1);
            setRowSelection({});
          }}
          className="w-52"
        />
        <Select
          value={attachmentFilter === "" ? "" : String(attachmentFilter)}
          onChange={(e) => {
            const v = e.target.value;
            setAttachmentFilter(v === "" ? "" : v === "true");
            setPage(1);
            setRowSelection({});
          }}
          className="w-40"
        >
          <option value="">添付ファイル: 全て</option>
          <option value="true">添付あり</option>
          <option value="false">添付なし</option>
        </Select>
      </div>

      {/* 一括操作バー */}
      {selectedCount > 0 && (
        <div className="flex items-center gap-3 rounded border border-blue-200 bg-blue-50 px-4 py-2">
          <span className="text-sm font-medium text-blue-800">
            {selectedCount} 件選択中
          </span>
          <div className="ml-auto flex gap-2">
            <Button
              variant="success"
              size="sm"
              onClick={() =>
                setConfirmAction({ type: "bulk-release", ids: selectedIds })
              }
              disabled={isPending}
            >
              <Unlock className="h-3.5 w-3.5 mr-1" />
              {selectedCount} 件を解放
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={() =>
                setConfirmAction({ type: "bulk-delete", ids: selectedIds })
              }
              disabled={isPending}
            >
              <Trash2 className="h-3.5 w-3.5 mr-1" />
              {selectedCount} 件を削除
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRowSelection({})}
            >
              選択解除
            </Button>
          </div>
        </div>
      )}

      {isError && (
        <div className="rounded border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          {error instanceof ApiError && error.status === 403
            ? "隔離メールの閲覧には operator 以上の権限が必要です。管理者にロールの付与を依頼してください。"
            : `エラーが発生しました: ${error instanceof Error ? error.message : "不明なエラー"}`}
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
              {table.getHeaderGroups().map((headerGroup) => (
                <TableRow key={headerGroup.id} className="hover:bg-transparent">
                  {headerGroup.headers.map((header) => (
                    <TableHead key={header.id}>
                      {flexRender(
                        header.column.columnDef.header,
                        header.getContext()
                      )}
                    </TableHead>
                  ))}
                </TableRow>
              ))}
            </TableHeader>
            <TableBody>
              {table.getRowModel().rows.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={columns.length}
                    className="text-center text-gray-500 py-10"
                  >
                    隔離メールはありません
                  </TableCell>
                </TableRow>
              ) : (
                table.getRowModel().rows.map((row) => (
                  <TableRow
                    key={row.id}
                    className="cursor-pointer"
                    data-selected={row.getIsSelected()}
                    onClick={() => navigate(`/quarantine/${row.original.id}`)}
                  >
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext()
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        )}
        {data && (
          <Pagination
            meta={data.meta}
            onPageChange={(p) => { setPage(p); setRowSelection({}); }}
            className="border-t border-gray-200 bg-gray-50 px-3 py-2"
          />
        )}
      </div>

      <Dialog
        open={confirmAction !== null}
        onClose={() => setConfirmAction(null)}
      >
        <DialogHeader>
          <DialogTitle>
            {confirmAction?.type === "release" && "隔離を解放しますか？"}
            {confirmAction?.type === "delete" && "メールを削除しますか？"}
            {confirmAction?.type === "bulk-release" && `${confirmAction.ids.length} 件を一括解放しますか？`}
            {confirmAction?.type === "bulk-delete" && `${confirmAction.ids.length} 件を一括削除しますか？`}
          </DialogTitle>
          <DialogDescription>
            {confirmAction?.type === "release" &&
              `「${confirmAction.subject}」を隔離から解放して配送します。`}
            {confirmAction?.type === "delete" &&
              `「${confirmAction.subject}」を完全に削除します。この操作は取り消せません。`}
            {confirmAction?.type === "bulk-release" &&
              `選択した ${confirmAction.ids.length} 件のメールを解放して配送します。`}
            {confirmAction?.type === "bulk-delete" &&
              `選択した ${confirmAction.ids.length} 件のメールを削除します。この操作は取り消せません。`}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => setConfirmAction(null)}>
            キャンセル
          </Button>
          <Button
            variant={
              confirmAction?.type === "release" || confirmAction?.type === "bulk-release"
                ? "success"
                : "destructive"
            }
            onClick={executeAction}
            disabled={isPending}
          >
            {confirmAction?.type === "release" && "解放する"}
            {confirmAction?.type === "delete" && "削除する"}
            {confirmAction?.type === "bulk-release" && "一括解放する"}
            {confirmAction?.type === "bulk-delete" && "一括削除する"}
          </Button>
        </DialogFooter>
      </Dialog>
    </div>
  );
}
