# セットアップガイド

**はじめに必ず読んでください:** [システム概要と前提アーキテクチャ](./overview.md)
— MailShield の位置付けと、導入前に必要な外部コンポーネントを説明します。

---

| ドキュメント | 説明 |
|------------|------|
| [システム概要と前提アーキテクチャ](./overview.md) | MailShield の位置付け・必要な MTA・インフラ要件 |
| [クイックスタート（Docker）](./quick-start.md) | 最短で起動する手順（開発・評価用） |
| [Docker プロファイル](./profiles.md) | 起動するコンポーネントの選択方法 |
| [開発用 MTA の設定](./mta-docker.md) | dev プロファイルの Postfix + Rspamd |
| [自前 MTA との連携](./mta-self-managed.md) | 既存の Postfix/Sendmail を使う場合の要件 |
| [バイナリインストール](./binary-install.md) | Docker を使わないセットアップ |
| [アップグレード](./upgrade.md) | バージョンアップ手順 |
