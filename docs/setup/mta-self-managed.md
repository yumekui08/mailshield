# 自前 MTA の接続方法

自前の MTA（Postfix / Sendmail 等）を使って MailShield に接続する場合の
要件と設定例を説明します。

同梱の MTA を使う場合は [mta-docker.md](./mta-docker.md) を参照してください。

---

## MailShield が MTA に求める要件

smtp-gateway は以下を前提としています。

### 1. メールの転送（after-queue content filter）

MTA は受信したメールを **そのまま** smtp-gateway の SMTP エンドポイントに転送してください。

| 項目 | 値 |
|------|---|
| 転送先ホスト | smtp-gateway のホスト名または IP |
| 転送先ポート | 10025（デフォルト。`config/mailshield.yaml` の `server.smtp_port` で変更可） |
| プロトコル | SMTP（TLS 不要。同一信頼ネットワーク内を想定） |

### 2. 再インジェクトの受け付け

smtp-gateway が処理を完了したメールは、MTA の **再インジェクトポート** に戻されます。

| 項目 | 値 |
|------|---|
| 受け付けポート | 10026（MTA 側で開けること） |
| content_filter | 空（ループ防止） |
| milter | 無効（二重適用防止） |

### 3. Authentication-Results ヘッダの付与

smtp-gateway は `Authentication-Results` ヘッダから SPF / DKIM / DMARC / ARC の
検証結果を読み取ります。MTA または milter（Rspamd 等）がこのヘッダを付与してください。

#### ヘッダ形式

```
Authentication-Results: mail.example.com;
    spf=pass smtp.mailfrom=sender@external.example;
    dkim=pass header.d=external.example header.s=default;
    dmarc=pass header.from=external.example;
    arc=none
```

#### smtp-gateway が参照するフィールド

| フィールド | 意味 |
|-----------|------|
| `spf=` | `pass` / `fail` / `softfail` / `neutral` / `none` |
| `dkim=` | `pass` / `fail` / `none` |
| `dmarc=` | `pass` / `fail` / `none` |
| `arc=` | `pass` / `fail` / `none` |

ヘッダが存在しない場合、各フィールドは `none` として扱われます。

### 4. 接続元の信頼設定

smtp-gateway は `config/mailshield.yaml` の `server.trusted_sources` に
登録されたホスト名または IP からのみ接続を受け付けます。

```yaml
server:
  trusted_sources:
    - postfix           # ホスト名
    - 192.168.1.10      # IP アドレスも可
```

---

## Postfix での設定例

### main.cf

```
# 受信するドメイン
relay_domains = example.com

# after-queue content filter として smtp-gateway に転送
# MailShield が同じホストで動作している場合: localhost
# 別ホスト（VM・コンテナ等）の場合: そのホスト名/IP
content_filter = smtp:[<mailshield-host>]:10025

# milter（Rspamd 等）
smtpd_milters     = inet:rspamd-host:11332
non_smtpd_milters = inet:rspamd-host:11332
milter_default_action = accept
milter_protocol = 6

# メッセージサイズ（mailshield.yaml の max_message_size_mb と合わせること）
message_size_limit = 52428800
```

### master.cf（再インジェクトポート 10026）

```
# smtp-gateway からの再インジェクトを受け付けるポート
# content_filter と milter を空にしてループと二重適用を防ぐ
10026  inet  n  -  n  -  -  smtpd
  -o content_filter=
  -o smtpd_milters=
  -o non_smtpd_milters=
  -o mynetworks=<mailshield のIPアドレス>/32
  -o smtpd_recipient_restrictions=permit_mynetworks,reject
  -o local_header_rewrite_clients=
  -o smtpd_helo_restrictions=
  -o smtpd_client_restrictions=
  -o smtpd_sender_restrictions=
```

---

## Rspamd での設定例

Rspamd を milter として使う場合、MailShield では以下のモジュールのみを有効にしてください。

### 有効にするモジュール

```conf
# local.d/dkim.conf
check_local = true;
check_authed = true;
```

```conf
# local.d/spf.conf
check_local = true;
check_authed = true;
```

```conf
# local.d/dmarc.conf
check_local = true;
check_authed = true;
```

```conf
# local.d/arc.conf
enabled = true;
check_local = true;
check_authed = true;
```

```conf
# local.d/milter_headers.conf
use = ["authentication-results"];
skip_local = false;
skip_authenticated = false;
```

### 無効にするモジュール

スパム判定は MailShield が行うため、以下は無効化することを推奨します。

```conf
# local.d/rbl.conf
enabled = false;

# local.d/dkim_signing.conf（受信MTAでは署名不要）
enabled = false;

# local.d/neural.conf
enabled = false;

# local.d/fuzzy_check.conf
enabled = false;

# local.d/phishing.conf
enabled = false;

# local.d/antivirus.conf（MailShield の av-worker が担当）
enabled = false;

# local.d/url_redirector.conf
enabled = false;

# local.d/classifier-bayes.conf
enabled = false;
```

### Rspamd によるメール拒否の無効化

Rspamd のスコアリングモジュールを無効化すれば実質的にメールは拒否されませんが、
念のため settings で明示的に制御することもできます。

```conf
# local.d/settings.conf（信頼済み MTA のIPを指定）
trusted_mta {
  priority = high;
  from_ip = ["<MailShield の送信元 IP>/32"];
  apply "default" {
    actions {
      reject   = null;
      "add header" = null;
      greylist = null;
    }
  }
}
```

---

## その他の MTA（sendmail, exim 等）

### 共通要件まとめ

1. 受信メールを `<mailshield-host>:10025` に SMTP 転送する
2. smtp-gateway からの戻りを `10026` で受け付ける（content_filter なし）
3. Rspamd（または同等の milter）で `Authentication-Results` ヘッダを付与する
4. MailShield の接続元 IP を `trusted_sources` に登録する

これらを満たせば、MTA の種類は問いません。
