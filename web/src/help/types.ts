// 画面内マニュアル / ガイドツアーのコンテンツモデル。
//
// 画面に機能を追加・変更したときは、対応する HelpContent（src/help/content.ts）も
// 必ず更新すること（CLAUDE.md のドキュメント更新ルール参照）。

export interface HelpSection {
  /** 見出し（例: "この画面でできること"） */
  heading: string;
  /** 箇条書き項目 */
  items: string[];
}

export interface TourStep {
  /**
   * ハイライト対象の CSS セレクタ（通常 `[data-help="xxx"]`）。
   * 見つからない場合は画面中央にツールチップだけを表示する（グレースフルデグレード）。
   */
  target?: string;
  title: string;
  body: string;
}

export interface HelpContent {
  /** パネルのタイトル（通常は画面名） */
  title: string;
  /** 画面の概要（1〜2 文） */
  summary: string;
  /** 構造化された説明セクション */
  sections: HelpSection[];
  /** 任意のガイドツアー（スポットライト形式）。未定義なら「ガイドを開始」は出ない */
  tour?: TourStep[];
}
