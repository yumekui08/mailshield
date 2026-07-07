import { useEffect, useMemo, useState } from "react";
import type { PageMeta } from "../types";

// API がページングをサポートしない一覧（ユーザー・メールボックス・API キー・承認）向けの
// クライアントサイドページング。フィルター適用後の配列を渡すこと。
export function usePagedList<T>(items: T[] | undefined, perPage = 20) {
  const [page, setPage] = useState(1);
  const total = items?.length ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / perPage));

  // フィルター変更等で総件数が減り現在ページが範囲外になったら末尾ページに戻す
  useEffect(() => {
    if (page > totalPages) setPage(totalPages);
  }, [page, totalPages]);

  const pageItems = useMemo(
    () => (items ?? []).slice((page - 1) * perPage, page * perPage),
    [items, page, perPage]
  );

  const meta: PageMeta = {
    total,
    page,
    per_page: perPage,
    total_pages: totalPages,
  };

  return { pageItems, meta, page, setPage };
}
