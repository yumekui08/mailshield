# 006: SMTP ホップ（after-queue content filter）の堅持・milter 化はしない

## 決定

MTA との接続方式は after-queue content filter（SMTP 中継ホップ）を堅持する（ADR 002 の再確認）。
milter プロトコルによる before-queue 統合への置き換えは行わない。

## 理由

- 本ソフトの主要機能（承認フロー・隔離・添付分離・送信ディレイ）はすべて
  「メールを一度預かり、後から配送する」機能であり、これはゲートウェイホップの
  アーキテクチャそのもの。milter は通過するメールに介入するモデルのため、
  保留系機能とは根本的に相性が悪い（HOLD キュー操作の密結合か、
  DISCARD + 自前配送 = content filter の再発明になる）
- SMTP を喋れる MTA なら何でも前段にできる（Postfix 以外の MTA・既存ゲートウェイとの多段構成）。
  milter は対応 MTA（Postfix/sendmail）に限定される
- gateway 停止時もメールは前段 Postfix のキューに滞留し、451 でリトライされる（メール消失なし）
- milter の利点である「SMTP セッション中の拒否（backscatter 回避）」は、
  エッジの Rspamd（milter）が既にその層を担っており、多層構成として完結している

## 補足

将来 MailShield 自身の判定による SMTP 時拒否がどうしても必要になった場合は、
content filter の置き換えではなく「追加の接続方式」として milter フロントエンドを併設する。
