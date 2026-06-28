# セットアップガイド

**はじめに必ず読んでください:** [システム概要と前提アーキテクチャ](./overview.md)
— MailShield の位置付けと、導入前に必要な外部コンポーネントを説明します。

---

## セットアップの流れ

```
1. MTA のセットアップ       → mta-self-managed.md
        ↓
2. MailShield の設定        → mailshield-config.md  ★ メインドキュメント
        ↓
3. ワーカー・ポリシーの調整  → ../guide/workers.md / ../guide/policy.md
```

---

## ドキュメント一覧

| ドキュメント | 説明 |
|------------|------|
| [システム概要と前提アーキテクチャ](./overview.md) | MailShield の位置付け・必要な MTA・インフラ要件 |
| [クイックスタート](./quick-start.md) | 最短で起動する手順 |
| **[MailShield 設定ガイド](./mailshield-config.md)** | **全設定項目のステップバイステップ解説（メイン）** |
| [Docker プロファイル](./profiles.md) | 起動するコンポーネントの選択方法 |
| [MTA との連携](./mta-self-managed.md) | 自前 Postfix / Rspamd の要件と設定例 |
| [MTA 設定例（examples/mta/）](./mta-docker.md) | examples/ フォルダの Postfix + Rspamd 設定ファイルの解説 |
| [バイナリインストール](./binary-install.md) | Docker を使わないセットアップ |
| [アップグレード](./upgrade.md) | バージョンアップ手順 |
| [ARC 署名統合](./arc-integration.md) | Exchange Online / Google Workspace への ARC 登録手順 |
