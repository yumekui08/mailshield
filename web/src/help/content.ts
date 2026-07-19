import type { HelpContent } from "./types";

// 画面ごとの組み込みマニュアル / ガイドツアー。
//
// ⚠️ 画面に機能を追加・変更したら、対応するここのエントリも必ず更新すること。
//    新しい画面を追加したら help キーを追加し、helpKeyForPath のマッピングも足す。

export type HelpKey =
  | "dashboard"
  | "messages"
  | "messageDetail"
  | "quarantine"
  | "quarantineDetail"
  | "approvals"
  | "approvalDetail"
  | "delayed"
  | "mailboxes"
  | "users"
  | "policy"
  | "workerInstances"
  | "variables"
  | "simulate"
  | "apiKeys"
  | "auditLogs";

export const helpContent: Record<HelpKey, HelpContent> = {
  dashboard: {
    title: "ダッシュボード",
    summary:
      "受信・処理したメールの件数や傾向をひと目で把握する画面です。期間ごとの配送・隔離・拒否の内訳と時系列グラフを表示します。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "配送 / 隔離 / 拒否 / 承認待ちなどステータス別の件数を確認する",
          "日別の処理件数グラフで傾向を把握する",
          "viewer ロールでは自分が閲覧できるメールボックス範囲に絞られる",
        ],
      },
      {
        heading: "ヒント",
        items: [
          "件数カードから各一覧画面へ辿ると詳細を確認できます。",
        ],
      },
    ],
    tour: [
      {
        title: "ダッシュボードへようこそ",
        body: "ここでは MailShield が処理したメールの全体像を確認できます。まず主要な指標から見ていきましょう。",
      },
      {
        target: '[data-help="dashboard-stats"]',
        title: "処理サマリ",
        body: "配送・隔離・拒否・承認待ちなど、ステータス別の件数がカードで表示されます。",
      },
      {
        target: '[data-help="dashboard-chart"]',
        title: "時系列グラフ",
        body: "日別の処理件数の推移です。急増・急減の傾向を把握するのに使います。",
      },
    ],
  },

  messages: {
    title: "メール処理ログ",
    summary:
      "MailShield が受信・処理したすべてのメールの一覧です。検査結果・変換結果・最終アクションを 1 通ごとに追跡できます。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "ステータス（配送 / 隔離 / 拒否など）や方向（受信 / 送信）で絞り込む",
          "行をクリックしてメール詳細（検査結果・ポリシー判定）を開く",
          "ページングで過去のログを辿る",
        ],
      },
    ],
    tour: [
      {
        title: "メール処理ログ",
        body: "処理済みメールを 1 通ずつ確認できる画面です。",
      },
      {
        target: '[data-help="messages-filter"]',
        title: "絞り込み",
        body: "ステータスや方向で一覧を絞り込めます。調査したい条件を選んでください。",
      },
      {
        target: '[data-help="messages-table"]',
        title: "一覧",
        body: "各行がメール 1 通です。クリックすると検査結果・変換結果・ポリシー判定を含む詳細が開きます。",
      },
    ],
  },

  messageDetail: {
    title: "メール詳細",
    summary:
      "1 通のメールについて、検査ワーカーのスコア・変換結果・ポリシー判定・原本 EML・添付ファイルを確認する画面です。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "各検査ワーカーの判定（スコア・検知フラグ）を確認する",
          "原本 EML をダウンロードする（operator 以上）",
          "分離された添付ファイルを確認・ダウンロードする",
        ],
      },
    ],
  },

  quarantine: {
    title: "隔離",
    summary:
      "ポリシーにより隔離されたメールの一覧です。内容を確認したうえで解放（再配送）または削除できます。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "隔離メールを解放（再配送）する / 削除する",
          "チェックボックスで複数選択して一括解放・一括削除する",
          "解放権限はメールボックスのロール（受信隔離=member / 送信隔離=owner）に従う",
        ],
      },
      {
        heading: "注意",
        items: [
          "解放するとメールは受信者へ配送されます。内容を確認してから操作してください。",
        ],
      },
    ],
    tour: [
      {
        title: "隔離メールの管理",
        body: "危険と判定され保留されたメールを確認し、解放または削除します。",
      },
      {
        target: '[data-help="quarantine-bulk"]',
        title: "一括操作",
        body: "行を複数選択すると、まとめて解放・削除できます。",
      },
    ],
  },

  quarantineDetail: {
    title: "隔離メール詳細",
    summary: "隔離された 1 通のメールの詳細を確認し、解放または削除する画面です。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "メールのメタデータと検査結果を確認する",
          "解放（再配送）または削除する",
          "添付ファイルを確認する",
        ],
      },
    ],
  },

  approvals: {
    title: "承認フロー",
    summary:
      "送信ポリシーにより承認者の判断を待っているメールの一覧です。承認すると配送、却下すると送信がキャンセルされます。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "件名・送信元・メール ID で検索し、ステータスで絞り込む（既定では却下を除外）",
          "複数の承認待ちを選択して一括承認・一括却下する",
          "行をクリックして本文・添付を確認してから判断する",
        ],
      },
      {
        heading: "承認者の決まり方",
        items: [
          "対象メールボックスに role=approver で割り当てられたユーザーが決定できます。",
          "却下すると内部送信者へ却下通知メールが送られます。",
        ],
      },
    ],
    tour: [
      {
        title: "承認フロー",
        body: "承認者の判断待ちのメールを処理する画面です。",
      },
      {
        target: '[data-help="approvals-filter"]',
        title: "検索・絞り込み",
        body: "件名や送信元で検索できます。既定では却下済みは非表示です。ステータスを切り替えると表示対象を変えられます。",
      },
      {
        target: '[data-help="approvals-table"]',
        title: "一覧と一括操作",
        body: "承認待ちの行はチェックボックスで選択でき、上部のバーから一括承認・却下できます。行をクリックすると本文を確認できます。",
      },
    ],
  },

  approvalDetail: {
    title: "承認詳細",
    summary:
      "承認待ちメールの本文・添付を確認し、承認（配送）または却下する画面です。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "メール本文を確認する（HTML はサンドボックス表示・テキストと切替可）",
          "添付ファイルを一覧・ダウンロードして中身を確認する",
          "コメントを添えて承認 / 却下する",
        ],
      },
    ],
  },

  delayed: {
    title: "送信ディレイ（送信待ち）",
    summary:
      "ポリシーにより一定時間保留されている送信メールの一覧です。誤送信に気づいたら送信前に取り消せます。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "保留中メールを即時送信する / 送信を取り消す",
          "viewer は自分が送信者のメールのみ操作できる",
        ],
      },
    ],
  },

  mailboxes: {
    title: "メールボックス管理",
    summary:
      "隔離メールの閲覧・解放権限や承認者を割り当てる単位です。共有アドレスと個人アドレスの両方をメールボックスとして登録します。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "アドレス・表示名で検索し、由来（手動 / LDAP）・有効状態・ロール未割り当てで絞り込む",
          "メールボックスにユーザーを役割付きで割り当てる（member=受信 / owner=送信 / approver=承認）",
          "割り当てはユーザー検索ポップアップから複数選んで追加できる",
        ],
      },
      {
        heading: "ロールの意味",
        items: [
          "member（受信担当）: このアドレス宛の隔離メールの閲覧・解放、添付ダウンロード",
          "owner（送信担当）: このアドレスからの送信隔離の閲覧・解放、送信ディレイ操作",
          "approver（承認担当）: 承認フローに回ったメールの承認 / 却下",
        ],
      },
    ],
    tour: [
      {
        title: "メールボックス管理",
        body: "誰がどのメールを扱えるかを決める、権限管理の中心となる画面です。",
      },
      {
        target: '[data-help="mailboxes-filter"]',
        title: "検索・絞り込み",
        body: "アドレスや表示名で検索し、由来やロール未割り当てで絞り込めます（例: approver 未設定の洗い出し）。",
      },
    ],
  },

  users: {
    title: "ユーザー管理",
    summary:
      "MailShield にログインできるユーザーと、その システムロール（admin / operator / viewer）を管理する画面です。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "ユーザーの追加・編集・無効化",
          "システムロール（admin / operator / viewer）の割り当て",
          "LDAP 連携時はディレクトリが真実の源になり、手動編集より優先される",
        ],
      },
    ],
  },

  policy: {
    title: "ポリシー編集",
    summary:
      "ルートごとの「検査結果 → アクション」ルールを編集する画面です。保存すると smtp-gateway に即時反映されます。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "ルートを切り替えてルールを追加・編集・並べ替える",
          "条件（検査ワーカーのスコア等）とアクション（配送 / 拒否 / 隔離 / 承認 / ディレイ / 転送）を設定する",
          "変更履歴からロールバックする",
        ],
      },
      {
        heading: "注意",
        items: [
          "フォールバックルール（condition: true）を必ず 1 つ用意してください（メール消失防止）。",
          "保存内容はサーバの policy.yaml に書き込まれ、再起動後も保持されます。",
        ],
      },
    ],
    tour: [
      {
        title: "ポリシー編集",
        body: "メールをどう扱うか（配送・隔離・拒否など）を決めるルールを編集します。",
      },
      {
        target: '[data-help="policy-routes"]',
        title: "ルート切替",
        body: "受信・送信などルートごとにルールセットが分かれています。編集したいルートを選んでください。",
      },
    ],
  },

  workerInstances: {
    title: "ワーカーインスタンス",
    summary:
      "「ワーカー型（検査・変換の実装）＋設定＋名前」をまとめた再利用可能な部品です。同じ型から用途別のインスタンスを複数作り、ルーティングから呼び出します。",
    sections: [
      {
        heading: "考え方（重要）",
        items: [
          "ワーカー型 = コードが提供する実装（av-worker・filesep-worker 等）。ここでは作れません。",
          "ワーカーインスタンス = 型に設定と名前を付けたもの。例: 同じ添付分離でも「内部向け」「外部向け」で設定違いを2つ作れます。",
          "ルーティングは、このインスタンスを alias で参照して検査・変換パイプラインに組み込みます。",
        ],
      },
      {
        heading: "3つの名前の使い分け",
        items: [
          "alias: 条件式・検査結果のキーに使う不変ハンドル（例 av_internal）。英小文字始まり。変更しないこと。",
          "表示名: 画面表示用。日本語可・いつでも変更可（例: 内部向けウイルス検査）。",
          "ワーカー型: 使う実装名（av-worker 等）。",
        ],
      },
      {
        heading: "設定（config）",
        items: [
          "ワーカー型ごとの固有設定を JSON で記述します。",
          "値には ${VAR} で設定変数を参照できます（設定ロード時に展開）。",
          "検査ワーカーの結果は alias でキーされ、ポリシー条件から alias.detected / alias.score で参照します。",
        ],
      },
      {
        heading: "落とし穴",
        items: [
          "alias を後から変えると、それを参照するポリシー条件が壊れます。最初に決めて固定してください。",
          "type=inspect は並列実行・type=transform は順序どおり直列実行されます（順序はルーティングで指定）。",
        ],
      },
    ],
  },

  variables: {
    title: "設定変数",
    summary:
      "ルーティング・ポリシー・ワーカー設定などから ${VAR} で参照する共有値です。同じ値を一箇所で管理し、環境ごとに差し替えられます。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "共有値（自組織ドメイン・スコア閾値など）を key=value で登録する",
          "設定内から ${KEY} で参照する（展開は設定ロード時に一度だけ・実行時コストなし）",
          "例: 受信判定の TO ドメインと送信判定の FROM ドメインを 1 つの INTERNAL_DOMAIN で管理",
        ],
      },
      {
        heading: "重要な注意",
        items: [
          "パスワード等のシークレットはここに入れないこと。値は平文表示・エクスポート対象です。",
          "シークレットは OS 環境変数のままにし、設定からは名前参照だけにします。",
          "変数を変更すると新しい設定バージョンとして扱われ、他の設定変更と同様に反映されます。",
        ],
      },
    ],
  },

  simulate: {
    title: "ポリシーシミュレーション",
    summary:
      "サンプルのメール条件を入力し、現在のポリシーがどのアクションを返すかを実際に配送せず確認する画面です。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "件名・送信元・検査スコア等を指定してポリシー評価を試す",
          "どのルールにマッチし、どのアクションになるかを確認する",
        ],
      },
    ],
  },

  apiKeys: {
    title: "API キー",
    summary:
      "外部システムから MailShield の API を呼ぶための API キーを発行・失効する画面です（admin のみ）。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "API キーを発行する（発行時のみ全体が表示されるので保管すること）",
          "不要になったキーを失効する",
        ],
      },
    ],
  },

  auditLogs: {
    title: "監査ログ",
    summary:
      "ログイン・承認・隔離操作・設定変更など、管理操作の記録を確認する画面です（admin のみ）。",
    sections: [
      {
        heading: "この画面でできること",
        items: [
          "いつ・誰が・何をしたかを時系列で確認する",
          "イベント種別で絞り込む",
        ],
      },
    ],
  },
};

// パス名から help キーを求める。動的セグメント（/messages/:id 等）は詳細キーへ寄せる。
export function helpKeyForPath(pathname: string): HelpKey | null {
  const p = pathname.replace(/\/+$/, "") || "/";
  if (p === "/") return "dashboard";
  if (/^\/messages\/[^/]+$/.test(p)) return "messageDetail";
  if (p === "/messages") return "messages";
  if (/^\/quarantine\/[^/]+$/.test(p)) return "quarantineDetail";
  if (p === "/quarantine") return "quarantine";
  if (/^\/approvals\/[^/]+$/.test(p)) return "approvalDetail";
  if (p === "/approvals") return "approvals";
  if (p === "/delayed") return "delayed";
  if (p === "/mailboxes") return "mailboxes";
  if (p === "/users") return "users";
  if (p === "/policy") return "policy";
  if (p === "/worker-instances") return "workerInstances";
  if (p === "/variables") return "variables";
  if (p === "/simulate") return "simulate";
  if (p === "/api-keys") return "apiKeys";
  if (p === "/audit-logs") return "auditLogs";
  return null;
}
