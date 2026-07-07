import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from "lucide-react";
import { cn } from "../lib/utils";
import type { PageMeta } from "../types";

interface PaginationProps {
  meta: PageMeta;
  onPageChange: (page: number) => void;
  className?: string;
}

function PageButton({
  onClick,
  disabled,
  active,
  children,
  label,
}: {
  onClick: () => void;
  disabled?: boolean;
  active?: boolean;
  children: React.ReactNode;
  label?: string;
}) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex h-7 min-w-7 items-center justify-center rounded border px-1.5 text-xs tabular-nums transition-colors",
        active
          ? "border-blue-700 bg-blue-700 font-medium text-white"
          : "border-gray-300 bg-surface text-gray-600 hover:bg-gray-100 hover:text-gray-900",
        "disabled:pointer-events-none disabled:opacity-40"
      )}
    >
      {children}
    </button>
  );
}

// 一覧テーブルのフッターに置くページネーション。
// 件数レンジ表示（全 N 件中 X–Y 件）+ 先頭/前/ページ番号/次/末尾。
// 1 ページに収まる場合も件数表示は残す（ボタンは非表示）。
export function Pagination({ meta, onPageChange, className }: PaginationProps) {
  const { page, per_page, total, total_pages } = meta;
  const from = total === 0 ? 0 : (page - 1) * per_page + 1;
  const to = Math.min(page * per_page, total);

  const pages: number[] = [];
  const start = Math.max(1, page - 2);
  const end = Math.min(total_pages, page + 2);
  for (let i = start; i <= end; i++) {
    pages.push(i);
  }

  return (
    <div className={cn("flex items-center justify-between gap-4", className)}>
      <div className="text-xs tabular-nums text-gray-500">
        全 {total.toLocaleString()} 件中 {from.toLocaleString()}–{to.toLocaleString()} 件を表示
      </div>

      {total_pages > 1 && (
        <div className="flex items-center gap-1">
          <PageButton onClick={() => onPageChange(1)} disabled={page <= 1} label="最初のページ">
            <ChevronsLeft className="h-3.5 w-3.5" />
          </PageButton>
          <PageButton onClick={() => onPageChange(page - 1)} disabled={page <= 1} label="前のページ">
            <ChevronLeft className="h-3.5 w-3.5" />
          </PageButton>

          {start > 1 && <span className="px-1 text-xs text-gray-400">…</span>}

          {pages.map((p) => (
            <PageButton key={p} onClick={() => onPageChange(p)} active={p === page}>
              {p}
            </PageButton>
          ))}

          {end < total_pages && <span className="px-1 text-xs text-gray-400">…</span>}

          <PageButton
            onClick={() => onPageChange(page + 1)}
            disabled={page >= total_pages}
            label="次のページ"
          >
            <ChevronRight className="h-3.5 w-3.5" />
          </PageButton>
          <PageButton
            onClick={() => onPageChange(total_pages)}
            disabled={page >= total_pages}
            label="最後のページ"
          >
            <ChevronsRight className="h-3.5 w-3.5" />
          </PageButton>
        </div>
      )}
    </div>
  );
}
