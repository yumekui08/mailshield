# セットアップガイド

MailShield のインストールと初期設定に関するドキュメントの索引。

## 目的別ガイド

| やりたいこと | 参照するドキュメント | 所要時間の目安 |
|-------------|-------------------|--------------|
| まず動かして評価したい（MTA 不要） | [クイックスタート](./quick-start.md) | 約 15 分 |
| Docker Compose で本番導入したい | [システム概要](./overview.md) → [MTA との連携](./mta-self-managed.md) → [MailShield 設定ガイド](./mailshield-config.md) | 半日〜 |
| Docker を使わずに導入したい | [バイナリインストール](./binary-install.md) | 半日〜 |
| 起動するコンポーネントを調整したい | [Docker Compose プロファイル](./profiles.md) | — |
| バージョンアップしたい | [アップグレード](./upgrade.md) | 変更内容による |
| ARC 署名を有効化したい | [ARC 署名統合](./arc-integration.md) | 1〜2 時間（DNS 反映待ち含む） |

## 本番導入の流れ

```
0. システム概要と前提アーキテクチャの理解    → overview.md   ★ 必読
        ↓
1. 受信 MTA（Postfix + Rspamd）のセットアップ → mta-self-managed.md
        ↓
2. MailShield 本体の設定と起動              → mailshield-config.md
        ↓
3. ワーカー・ポリシーの調整                 → ../guide/workers.md / ../guide/policy.md
```

## ドキュメント一覧

| ドキュメント | 内容 |
|------------|------|
| [システム概要と前提アーキテクチャ](./overview.md) | MailShield の位置付け・システム要件・必要な外部コンポーネント |
| [クイックスタート](./quick-start.md) | 外部 MTA なしで単一マシン上に評価環境を構築する |
| [MailShield 設定ガイド](./mailshield-config.md) | 本番導入時の全設定項目のステップバイステップ解説 |
| [Docker Compose プロファイル](./profiles.md) | 起動するコンポーネントの選択・外部サービスへの切り替え |
| [MTA との連携](./mta-self-managed.md) | 自前 Postfix / Rspamd の要件と設定例・DNS 設定 |
| [バイナリインストール](./binary-install.md) | Docker を使わないビルド・配置・systemd 常駐化 |
| [アップグレード](./upgrade.md) | バックアップ・マイグレーション・ロールバック手順 |
| [ARC 署名統合](./arc-integration.md) | ARC 鍵生成・DNS 登録・Exchange Online / Google Workspace 連携 |

## 表記規約

セットアップガイド全体で以下の規約を用いる。

- **プレースホルダー** — `<mariadb-host>` のように山括弧で示した値は環境に合わせて置き換える。`${DOMAIN}` のようにドル記号付きで示した値は、ガイド冒頭で `export` したシェル変数をそのまま利用できる
- **コマンド実行環境** — 特に断りがない限り、コマンドはリポジトリのルートディレクトリで実行する
- **注意書き** — 重要度に応じて次のアラート表記を使用する

> [!NOTE]
> 補足情報。読み飛ばしても手順は完了できる。

> [!IMPORTANT]
> 手順の成否に関わる重要な情報。

> [!WARNING]
> 誤るとメールの消失・ループ・セキュリティ問題につながる情報。
