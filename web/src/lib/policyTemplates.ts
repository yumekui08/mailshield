import type { PolicyRule } from "../types";

// シナリオテンプレート: よくある構成を定型ルールとして提供する。
// UI から選ぶと現在のルール末尾（デフォルトルールの手前）に挿入される。
export interface PolicyTemplate {
  id: string;
  name: string;
  description: string;
  rules: PolicyRule[];
}

export const POLICY_TEMPLATES: PolicyTemplate[] = [
  {
    id: "external-tag",
    name: "外部メールに [EXTERNAL] タグ",
    description:
      "受信した外部メールの件名に [EXTERNAL] を付け、識別用ヘッダーを追加する（非終端）。",
    rules: [
      {
        name: "tag_external",
        description: "受信外部メールの可視化",
        condition: "mail.direction == inbound",
        actions: [
          { type: "add_subject_prefix", value: "[EXTERNAL] " },
          { type: "add_header", name: "X-MailShield-Origin", value: "external" },
        ],
      },
    ],
  },
  {
    id: "freemail-approval",
    name: "フリーメール宛の送信を承認へ",
    description:
      "個人フリーメール宛の外部送信を上長承認に回す（要 lists.freemail 定義）。",
    rules: [
      {
        name: "freemail_to_approval",
        description: "フリーメール宛送信の誤送信対策",
        condition: "mail.direction == outbound && mail.to_domains in_list freemail",
        action: "approval",
      },
    ],
  },
  {
    id: "after-hours-delay",
    name: "業務時間外の送信をディレイ",
    description:
      "夜間・週末（UTC 基準）の送信を 10 分保留し、取消の猶予を作る。",
    rules: [
      {
        name: "after_hours_delay",
        description: "業務時間外送信の保留（UTC）",
        condition:
          "mail.direction == outbound && (mail.hour >= 18 || mail.hour < 8 || mail.weekday == sat || mail.weekday == sun)",
        actions: [{ type: "delay", delay_minutes: 10 }],
      },
    ],
  },
  {
    id: "strip-exe",
    name: "実行ファイル添付を除去",
    description: "受信メールから実行系の添付ファイルを除去してから配送する（非終端）。",
    rules: [
      {
        name: "strip_dangerous_attachments",
        description: "危険な拡張子の添付除去",
        condition: "mail.direction == inbound && mail.has_attachment == true",
        actions: [{ type: "strip_attachments", value: "exe,scr,com,bat,cmd,js,vbs,jar,ps1" }],
      },
    ],
  },
];
