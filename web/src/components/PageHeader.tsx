import type { ReactNode } from "react";
import { useLocation } from "react-router-dom";
import { HelpButton } from "../help/HelpButton";
import { helpKeyForPath } from "../help/content";

interface PageHeaderProps {
  title: string;
  description?: string;
  /** 一覧の総件数。undefined の間（ロード中）は表示しない */
  count?: number;
  /** 右端に置くアクション（新規作成ボタン等） */
  actions?: ReactNode;
}

// 全ページ共通のヘッダー。タイトル・説明・件数・アクションを一列に揃え、
// 下ヘアラインでコンテンツと区切る。右端には現在の画面に対応するヘルプ（?）ボタンを
// 自動表示する（src/help/content.ts に登録がある画面のみ）。
export function PageHeader({ title, description, count, actions }: PageHeaderProps) {
  const { pathname } = useLocation();
  const helpKey = helpKeyForPath(pathname);

  return (
    <div className="flex items-end justify-between gap-4 border-b border-gray-200 pb-4">
      <div>
        <div className="flex items-baseline gap-2.5">
          <h1 className="text-lg font-semibold tracking-tight text-gray-900">{title}</h1>
          {count !== undefined && (
            <span className="text-xs tabular-nums text-gray-400">
              全 {count.toLocaleString()} 件
            </span>
          )}
        </div>
        {description && <p className="mt-0.5 text-[13px] text-gray-500">{description}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {actions}
        {helpKey && <HelpButton helpKey={helpKey} />}
      </div>
    </div>
  );
}
