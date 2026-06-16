import { ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "./ui/button";
import type { PageMeta } from "../types";

interface PaginationProps {
  meta: PageMeta;
  onPageChange: (page: number) => void;
}

export function Pagination({ meta, onPageChange }: PaginationProps) {
  const { page, total_pages } = meta;

  const pages: number[] = [];
  const start = Math.max(1, page - 2);
  const end = Math.min(total_pages, page + 2);
  for (let i = start; i <= end; i++) {
    pages.push(i);
  }

  return (
    <div className="flex items-center justify-center gap-1">
      <Button
        variant="outline"
        size="icon"
        onClick={() => onPageChange(page - 1)}
        disabled={page <= 1}
        aria-label="前のページ"
      >
        <ChevronLeft className="h-4 w-4" />
      </Button>

      {start > 1 && (
        <>
          <Button variant="outline" size="icon" onClick={() => onPageChange(1)}>
            1
          </Button>
          {start > 2 && (
            <span className="px-2 text-gray-400">…</span>
          )}
        </>
      )}

      {pages.map((p) => (
        <Button
          key={p}
          variant={p === page ? "default" : "outline"}
          size="icon"
          onClick={() => onPageChange(p)}
        >
          {p}
        </Button>
      ))}

      {end < total_pages && (
        <>
          {end < total_pages - 1 && (
            <span className="px-2 text-gray-400">…</span>
          )}
          <Button
            variant="outline"
            size="icon"
            onClick={() => onPageChange(total_pages)}
          >
            {total_pages}
          </Button>
        </>
      )}

      <Button
        variant="outline"
        size="icon"
        onClick={() => onPageChange(page + 1)}
        disabled={page >= total_pages}
        aria-label="次のページ"
      >
        <ChevronRight className="h-4 w-4" />
      </Button>
    </div>
  );
}
