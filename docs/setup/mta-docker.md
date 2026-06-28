# MTA 設定例（examples/mta/）

`examples/mta/` には Postfix + Rspamd を MailShield と連携させる設定ファイルのサンプルが入っています。
自前の MTA を設定する際の参考としてお使いください。

> MTA に求める要件と Postfix / Rspamd の詳細設定は
> [MTA との連携](./mta-self-managed.md) を参照してください。

---

## ファイル構成

```
examples/mta/
├── postfix/                    # 受信 MTA（SMTP :25 → content_filter → MailShield）
│   ├── Dockerfile
│   ├── main.cf.template        # 受信設定・content_filter 設定
│   ├── master.cf               # 再インジェクトポート（:10025）定義
│   └── docker-entrypoint.sh
│
├── postfix-submission/         # 送信 MTA（SMTP submission :587）
│   ├── Dockerfile
│   ├── main.cf.template
│   ├── master.cf
│   └── docker-entrypoint.sh
│
└── rspamd/
    └── local.d/                # Rspamd 設定（認証チェック専用・スパム判定無効）
        ├── spf.conf
        ├── dkim.conf
        ├── dmarc.conf
        ├── arc.conf
        ├── milter_headers.conf # Authentication-Results ヘッダの付与設定
        ├── rbl.conf            # 無効化（MailShield が担当）
        ├── antivirus.conf      # 無効化（av-worker が担当）
        └── ...
```

---

## postfix/main.cf.template の概要

```
# MailShield への転送（content_filter）
content_filter = smtp:[${MAILSHIELD_HOST}]:10024

# milter（Rspamd）
smtpd_milters     = inet:${RSPAMD_HOST}:11332
non_smtpd_milters = inet:${RSPAMD_HOST}:11332
milter_default_action = accept
milter_protocol = 6
```

`${MAILSHIELD_HOST}` / `${RSPAMD_HOST}` は `docker-entrypoint.sh` で環境変数から展開されます。

---

## postfix/master.cf の再インジェクトポート

```
# smtp-gateway からの再インジェクトを受け付けるポート
# content_filter と milter を空にしてループを防ぐ
10025  inet  n  -  n  -  -  smtpd
  -o content_filter=
  -o smtpd_milters=
  -o non_smtpd_milters=
  -o mynetworks=${MAILSHIELD_HOST}/32
  -o smtpd_recipient_restrictions=permit_mynetworks,reject
```

---

## rspamd/local.d/ の方針

同梱の設定では Rspamd を **認証チェック専用** として構成しています。

| モジュール | 状態 | 理由 |
|-----------|------|------|
| `spf` / `dkim` / `dmarc` / `arc` | 有効 | Authentication-Results ヘッダを付与するため |
| `milter_headers` | 有効 | ヘッダ付与の出力設定 |
| `rbl` / `neural` / `fuzzy_check` | 無効 | スパム判定は MailShield のワーカー層が担当 |
| `antivirus` | 無効 | ウイルス検査は av-worker（ClamAV）が担当 |
| `dkim_signing` | 無効 | 受信 MTA での署名は不要 |

この設定により Rspamd はメールを拒否せず、認証結果のヘッダ付与のみを行います。
