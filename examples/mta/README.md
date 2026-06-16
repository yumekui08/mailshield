# MTA 参考設定（開発・検証用）

このディレクトリには、MailShield と連携する MTA (Postfix) および
スパムフィルター (Rspamd) の参考設定が含まれています。

> **注意**: これらは開発・動作確認用の設定であり、本番環境への直接適用を想定していません。
> 本番環境では組織の MTA（Postfix・Exchange・Sendmail 等）を別途セットアップし、
> MailShield の `smtp-gateway` をコンテンツフィルターとして構成してください。
> 設定例は [docs/setup/mta-self-managed.md](../../docs/setup/mta-self-managed.md) を参照。

---

## ディレクトリ構成

```
examples/mta/
├── postfix/             受信 MTA（Postfix + Rspamd milter）
│   ├── Dockerfile
│   ├── main.cf.template
│   ├── master.cf
│   └── docker-entrypoint.sh
├── postfix-submission/  送信 MTA（内部ユーザー向け認証 SMTP）
│   ├── Dockerfile
│   ├── main.cf.template
│   ├── master.cf
│   └── docker-entrypoint.sh
└── rspamd/              Rspamd（SPF/DKIM/DMARC/ARC 検証のみ）
    └── local.d/
        ├── arc.conf
        ├── milter_headers.conf
        └── ...（スパム系モジュールは無効化済み）
```

## 開発環境での使い方

`docker-compose.yml` の `dev` プロファイルに含まれています。

```bash
make dev-up   # MTA + Mailpit + MailShield コアを起動
```

## Rspamd の役割

このサンプル設定では Rspamd を SPF/DKIM/DMARC/ARC の**検証のみ**に使用します。
スパムスコアリングは MailShield のワーカー層が担うため、以下のモジュールは無効化しています:

- RBL（IP ブロックリスト）
- Bayesian 分類
- Fuzzy ハッシュ
- ニューラルネット
- フィッシング検知
- URL レピュテーション

Rspamd は検証結果を `Authentication-Results:` ヘッダに付与し、
MailShield はそのヘッダを `header-inspector` ワーカーで読み取ります。
