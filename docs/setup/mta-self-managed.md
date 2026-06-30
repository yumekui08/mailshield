# 自前 MTA（Postfix + Rspamd）の接続設定

このガイドでは、Postfix + Rspamd を自前で用意して MailShield と連携させる手順を説明します。  
Docker 同梱の MTA を使う場合は [mta-docker.md](./mta-docker.md) を参照してください。

---

## 構成の概要

```
[外部送信者]
     |
     | SMTP :25
     v
[Postfix（受信 MTA）]  ←→  [Rspamd（milter）]
     |                          SPF/DKIM/DMARC 検証
     | SMTP :10024              Authentication-Results ヘッダ付与
     v
[smtp-gateway（MailShield）]
     |
     | SMTP :10025（再インジェクト）
     v
[Postfix（再インジェクト受け付け）]
     |
     | SMTP :25（内部配送）
     v
[最終 MTA または社内メールサーバー]
```

**ポイント**
- Postfix と smtp-gateway は同一ホストに同居しても、別ホストに分けても動作します
- Rspamd は milter として Postfix に接続し、認証チェック結果を `Authentication-Results` ヘッダに書き込みます
- smtp-gateway はそのヘッダを読んで SPF / DKIM / DMARC / ARC の結果をワーカーに渡します

---

## 前提条件

| 項目 | 要件 |
|------|------|
| OS | RHEL 9 / AlmaLinux 9 / Rocky Linux 9 または同等の RHEL 系 Linux |
| Postfix | 3.5 以上 |
| Rspamd | 3.0 以上 |
| smtp-gateway | MailShield のバイナリまたは Docker コンテナ |
| 開放ポート | 25（外部受信）、10024（content_filter）、10025（再インジェクト） |

---

## 1. Postfix の設定

### インストール

```bash
dnf install postfix
systemctl enable postfix
```

### 1-1. main.cf

`/etc/postfix/main.cf` に以下を追記します（既存の設定はそのままにして末尾に追加）。

```ini
# ─── ドメイン・ホスト名 ────────────────────────────────────────────
# 受信・中継するドメイン
mydomain = example.com
# このMTAのFQDN。SMTPのEHLO/HELOで返すホスト名
myhostname = mta.example.com
# エンベロープFromのデフォルトドメイン
myorigin = mta.example.com

# ─── インタフェース ────────────────────────────────────────────────
# 全インタフェースで受信する
inet_interfaces = all
inet_protocols = all

# ─── 受信ドメイン ─────────────────────────────────────────────────
# 中継を許可するドメイン一覧（別途ファイルで管理）
relay_domains = hash:/etc/postfix/vmail_domains

# ─── 最終配送先ルーティング ────────────────────────────────────────
# ドメインごとに最終 MTA を指定する（MX を引かず直接接続する）
transport_maps = hash:/etc/postfix/transport_maps

# ─── 許可ネットワーク ──────────────────────────────────────────────
# 自組織の IP レンジを指定する
mynetworks = 192.0.2.0/24, 127.0.0.0/8

# ─── スパム対策（外部接続制限） ────────────────────────────────────
smtpd_relay_restrictions =
    permit_mynetworks,
    reject_unauth_destination

# ─── after-queue content filter（MailShield への転送） ──────────────
# smtp-gateway が同一ホストにある場合: smtp:[127.0.0.1]:10024
# smtp-gateway が別ホストの場合: smtp:[<smtpgw-host>]:10024
content_filter = smtp:[127.0.0.1]:10024
syslog_name = postfix-in

# ─── メッセージサイズ ──────────────────────────────────────────────
# config/mailshield.yaml の max_message_size_mb（デフォルト 50）と合わせること
message_size_limit = 52428800

# ─── Rspamd milter 設定 ────────────────────────────────────────────
# Rspamd が同一ホストにある場合: inet:127.0.0.1:11332
# Rspamd が別ホストの場合: inet:<rspamd-host>:11332
smtpd_milters         = inet:127.0.0.1:11332
non_smtpd_milters     = inet:127.0.0.1:11332
milter_default_action = accept
milter_protocol       = 6
# DSN/バウンスメールにも milter を適用して Authentication-Results を付与する
internal_mail_filter_classes = bounce,notify
```

### 1-2. master.cf

`/etc/postfix/master.cf` に再インジェクトポートの定義を追加します。

```ini
# smtp-gateway が処理済みメールを戻すポート
# content_filter と milter を外してループ・二重適用を防ぐ
10025  inet  n  -  n  -  -  smtpd
  -o content_filter=
  -o receive_override_options=no_header_body_checks
  -o smtpd_recipient_restrictions=permit_mynetworks,reject
  -o smtpd_helo_restrictions=
  -o smtpd_client_restrictions=
  -o smtpd_sender_restrictions=
  -o local_header_rewrite_clients=
  -o syslog_name=postfix-out
```

> **なぜ `receive_override_options=no_header_body_checks` が必要か**
>
> 再インジェクトポートで受け取ったメールは smtp-gateway が変換済みのため、
> ヘッダ/ボディチェック（milter 含む）を再適用すると二重処理になります。
> このオプションでスキップします。

### 1-3. relay_domains ファイル

受信を許可するドメインを列挙します。

```
# /etc/postfix/vmail_domains
example.com   OK
sub.example.com   OK
```

```bash
# ファイル更新後は必ず postmap を実行
postmap /etc/postfix/vmail_domains
```

### 1-4. transport_maps ファイル

再インジェクト後の最終配送先を指定します。  
**MX を引くとゲートウェイ自身を指してループします。直接 IP またはホスト名を `[ ]` で囲んで指定してください。**

```
# /etc/postfix/transport_maps
#
# 書式: <ドメイン>  smtp:[<ホスト名またはIP>]:<ポート>
# [ ] で囲むと MX を引かず A レコードで直接接続する
#
example.com       smtp:[mail.example.com]:25
sub.example.com   smtp:[mail.example.com]:25
```

```bash
postmap /etc/postfix/transport_maps
```

### 1-5. 設定の反映と確認

```bash
# 設定構文チェック
postfix check

# 再起動
systemctl restart postfix

# ログ確認
journalctl -u postfix -f
```

---

## 2. Rspamd の設定

Rspamd の役割は **SPF / DKIM / DMARC / ARC の検証と `Authentication-Results` ヘッダの付与のみ** です。  
スパムスコアリングは MailShield が担うため、スパム判定モジュールはすべて無効化します。

### インストール

```bash
# 公式リポジトリを追加してインストール（https://rspamd.com/downloads.html 参照）
curl https://rspamd.com/rpm-stable/centos-9/rspamd.repo -o /etc/yum.repos.d/rspamd.repo
dnf install rspamd
systemctl enable rspamd
```

### 2-1. options.inc（local_addrs の調整）

Rspamd はデフォルトで `192.168.0.0/16` 等のプライベートアドレスを「ローカル」とみなし、
SPF/DKIM/DMARC チェックをスキップします。  
テスト環境でプライベート IP アドレスの送信者に対しても検証を実行したい場合は、
該当するサブネットを `local_addrs` から外します。

```conf
# /etc/rspamd/local.d/options.inc（変更が必要な場合のみ作成）
#
# デフォルト:
# local_addrs = [192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12, ...];
#
# 例: 192.168.0.0/16 をローカル扱いから除外する（テスト環境向け）
local_addrs = [10.0.0.0/8, 172.16.0.0/12, fd00::/8, 169.254.0.0/16, fe80::/10];
```

> **本番環境では通常このファイルの変更は不要です。**  
> 外部から届くメール（パブリック IP を持つ送信者）は自動的に SPF/DKIM の検証対象になります。

### 2-2. milter_headers.conf

Postfix に渡すヘッダを `Authentication-Results` のみに絞ります。

```conf
# /etc/rspamd/local.d/milter_headers.conf

# smtp-gateway が読み取るヘッダ
use = ["authentication-results"];

# ローカル IP からのメールにもヘッダを付与する（テスト環境向け）
# 本番環境でローカルメールにヘッダ不要な場合はコメントアウト
# skip_local = false;
# skip_authenticated = false;
```

### 2-3. ARC の設定

ARC（Authenticated Received Chain）はメール転送中に認証情報が引き継げるプロトコルです。  
MailShield が本文を変換すると元の DKIM 署名が無効になりますが、ARC を使えば
「変換前は DKIM valid だった」という情報を次の MTA に引き継げます。  
詳細は [arc-integration.md](./arc-integration.md) を参照。

```conf
# /etc/rspamd/local.d/arc.conf

# ARC 検証・署名を有効化（Authentication-Results に arc=pass/fail を追加）
enabled = true;
check_local = true;
check_authed = true;
```

### 2-4. SPF / DKIM / DMARC の有効化

```conf
# /etc/rspamd/local.d/spf.conf
enabled = true;
# テスト環境でローカル IP からのメールも検証する場合は以下を有効化
# check_local = true;
# check_authed = true;
```

```conf
# /etc/rspamd/local.d/dkim.conf
enabled = true;
# check_local = true;
# check_authed = true;
```

```conf
# /etc/rspamd/local.d/dmarc.conf
enabled = true;
# check_local = true;
# check_authed = true;
```

### 2-5. スパム判定モジュールの無効化

スパム判定は MailShield が行うため、以下はすべて無効化します。

```conf
# /etc/rspamd/local.d/antivirus.conf
# ウイルス検査は MailShield の av-worker（ClamAV）が担当する
enabled = false;
```

```conf
# /etc/rspamd/local.d/classifier-bayes.conf
# ベイズ分類器（学習データなしでは機能しない）
enabled = false;
```

```conf
# /etc/rspamd/local.d/neural.conf
# ニューラルネット分類器（学習データなしでは機能しない）
enabled = false;
```

```conf
# /etc/rspamd/local.d/fuzzy_check.conf
# ファジーハッシュ（Rspamd 共有サーバへの外部通信が発生する）
enabled = false;
```

```conf
# /etc/rspamd/local.d/phishing.conf
# URL チェックは MailShield の url-worker が担当する
enabled = false;
```

```conf
# /etc/rspamd/local.d/rbl.conf
# DNS ブロックリスト（スパム判定は MailShield が担当する）
enabled = false;
```

```conf
# /etc/rspamd/local.d/url_redirector.conf
# URL リダイレクト追跡（url-worker が担当する）
enabled = false;
```

### 2-6. 信頼済み MTA の設定

smtp-gateway を含む信頼済み送信元からのメールに対して、
Rspamd のスパムアクション（reject / greylist / add-header）を無効化します。

```conf
# /etc/rspamd/local.d/settings.conf

trusted_mta {
  priority = high;
  # smtp-gateway を含む、信頼する送信元ネットワーク
  from_ip = ["127.0.0.1/8", "192.0.2.0/24"];
  apply "default" {
    actions {
      reject      = null;   # メールを拒否しない
      "add header" = null;  # スパム判定ヘッダを付与しない
      greylist    = null;   # グレイリスト保留しない
    }
  }
}
```

### 2-7. milter プロキシ設定

```conf
# /etc/rspamd/local.d/worker-proxy.inc

milter  = yes;
timeout = 120s;

upstream "local" {
  default = yes;
  hosts   = "localhost:11333";
}

# Postfix と Rspamd が同一ホストの場合は "localhost:11332" でも可
# 別ホストから接続される場合は全インタフェースにバインド
bind_socket = "*:11332";
```

### 2-8. コントローラー設定（管理画面）

```conf
# /etc/rspamd/local.d/worker-controller.inc

# パスワードハッシュは rspamadm pw で生成する
password    = "<rspamadm pw で生成したハッシュ>";
bind_socket = "*:11334";
```

```bash
# パスワードハッシュの生成
rspamadm pw
# 対話式でパスワードを入力 → ハッシュが出力される
```

管理画面: `http://<rspamd-host>:11334`

### 2-9. Rspamd の再起動と確認

```bash
systemctl restart rspamd

# milter ポートが開いているか確認
ss -tlnp | grep 11332

# ログ確認
journalctl -u rspamd -f
```

---

## 3. DKIM 署名の設定

DKIM（DomainKeys Identified Mail）は送信メールに電子署名を付与し、
受信側が「このメールは本当に example.com が送ったものか」を検証できるようにします。

### 3-1. DKIM 鍵ペアの生成

`rspamadm dkim_keygen` コマンドで秘密鍵と DNS 用の TXT レコードを同時に生成します。

```bash
# 鍵ファイルの保存ディレクトリを準備
mkdir -p /etc/rspamd/keys
chown _rspamd:_rspamd /etc/rspamd/keys
chmod 750 /etc/rspamd/keys

# 鍵ペアを生成（セレクター名は任意。"dkim" や "mail" が一般的）
rspamadm dkim_keygen \
  -s dkim \
  -d example.com \
  -k /etc/rspamd/keys/dkim.example.com.private.key
```

出力例:

```
-----BEGIN RSA PRIVATE KEY-----
(秘密鍵の内容 → 自動で /etc/rspamd/keys/dkim.example.com.private.key に保存)
-----END RSA PRIVATE KEY-----

dkim._domainkey IN TXT ( "v=DKIM1; k=rsa; "
        "p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA..." ) ;
```

この `dkim._domainkey IN TXT (...)` の部分を DNS に登録します（ステップ 4 参照）。

```bash
# 鍵ファイルのパーミッション設定
chmod 440 /etc/rspamd/keys/dkim.example.com.private.key
chown _rspamd:_rspamd /etc/rspamd/keys/dkim.example.com.private.key
```

### 3-2. Rspamd で DKIM 署名を設定

```conf
# /etc/rspamd/local.d/dkim_signing.conf

# エンベロープ From が空のメール（バウンスメール等）にも署名する
allow_envfrom_empty = true;

# 秘密鍵のパス（$selector と $domain は Rspamd が自動展開）
# rspamadm dkim_keygen の -s と -d を一致させること
path     = "/etc/rspamd/keys/$selector.$domain.private.key";
selector = "dkim";

# ローカル IP・認証済みユーザーからのメールにも署名する
sign_local       = true;
sign_authenticated = true;

# DKIM 署名に使うドメインの取得元（MIME From を使用）
use_domain = "header";

# DNS の公開鍵と一致するか確認する（本番推奨）
check_pubkey = false;   # まず false でテストし、正常動作を確認してから true に
```

```bash
systemctl restart rspamd
```

---

## 4. DNS の設定

### 4-1. MX レコード

受信したいドメインの MX レコードにこの MTA を登録します。

```dns
; example.com のゾーンファイル（BIND 形式）
example.com.   IN  MX  10  mta.example.com.
mta.example.com.  IN  A   192.0.2.1
```

### 4-2. SPF レコード

このMTAのみからメールを送信する場合:

```dns
example.com.  IN  TXT  "v=spf1 ip4:192.0.2.1 -all"
```

複数の送信元がある場合:

```dns
example.com.  IN  TXT  "v=spf1 ip4:192.0.2.0/24 include:spf.sendgrid.net ~all"
```

| 修飾子 | 意味 |
|--------|------|
| `-all` | 拒否（リスト外からの送信を認めない） |
| `~all` | softfail（リスト外はフラグを立てるが受信する） |
| `+all` | 全許可（非推奨） |

### 4-3. DKIM レコード

`rspamadm dkim_keygen` の出力をそのまま DNS の TXT レコードとして登録します。

```dns
; セレクター "dkim"、ドメイン "example.com" の場合
; レコード名 = <セレクター>._domainkey.<ドメイン>
dkim._domainkey.example.com.  IN  TXT  (
  "v=DKIM1; k=rsa; "
  "p=MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA"
  "...（公開鍵をここに続ける）..."
)
```

> **DNS TXT レコードの長さ制限について**  
> 2048 bit 以上の RSA 鍵を使うと TXT レコードが 255 文字を超えます。  
> BIND では `( "部分1" "部分2" )` のように文字列を分割して記述します。  
> `rspamadm dkim_keygen` の出力はすでに分割済みの形式です。

登録後の確認:

```bash
# DKIM レコードが正しく配信されているか確認
dig TXT dkim._domainkey.example.com +short
```

### 4-4. DMARC レコード

DMARC は SPF と DKIM の結果をもとに「認証失敗時にどう処理するか」のポリシーを宣言します。

```dns
_dmarc.example.com.  IN  TXT  "v=DMARC1; p=none; rua=mailto:postmaster@example.com"
```

| `p=` の値 | 意味 |
|-----------|------|
| `none` | 何もしない（監視のみ） |
| `quarantine` | 迷惑メールフォルダに振り分け |
| `reject` | 受信拒否 |

**運用の進め方**: 最初は `p=none` でレポートを収集し、正規のメールが認証をパスできているか確認してから `quarantine` → `reject` に上げていきます。

---

## 5. MailShield の設定

### 5-1. config/mailshield.yaml

```yaml
server:
  # smtp-gateway への接続を許可するホスト/IP
  # Postfix が同一ホストの場合は 127.0.0.1 のみで十分
  # Postfix が別ホストの場合は Postfix の IP を追加する
  trusted_sources:
    - 127.0.0.1
    # - 192.0.2.1   # Postfix が別ホストの場合

reinject:
  # 処理済みメールを戻す先（Postfix の再インジェクトポート）
  host: localhost   # Postfix が別ホストの場合はそのホスト名/IP
  port: 10025
```

### 5-2. config/routes.d/10-inbound/route.yaml

受信ドメインを自組織のドメインに変更します。

```yaml
match:
  # 受信ドメインの正規表現
  to: "@(example\\.com|sub\\.example\\.com)$"
  direction: inbound
```

---

## 6. 動作確認

### 6-1. SMTP 接続テスト

```bash
# 通常メールのテスト（外部→内部）
swaks \
  --to user@example.com \
  --from sender@external.example \
  --server mta.example.com \
  --port 25 \
  --header "Subject: Test from external"

# smtp-gateway 直接テスト（Postfix をバイパス）
swaks \
  --to user@example.com \
  --from sender@external.example \
  --server 127.0.0.1 \
  --port 10024 \
  --header "Subject: Direct to smtp-gateway"
```

### 6-2. Authentication-Results ヘッダの確認

受信メールのヘッダに `Authentication-Results` が付与されているか確認します。  
Mailpit を使っている場合は `http://localhost:8025` → **Source** タブで確認できます。

```
Authentication-Results: mta.example.com;
    spf=pass smtp.mailfrom=sender@external.example;
    dkim=pass header.d=external.example header.s=dkim;
    dmarc=pass header.from=external.example;
    arc=none
```

| フィールド | 正常値 |
|-----------|--------|
| `spf=` | `pass` |
| `dkim=` | `pass`（送信元が DKIM 設定済みの場合） |
| `dmarc=` | `pass` |
| `arc=` | `pass`（ARC 設定済みの転送メールの場合）/ `none`（直接受信の場合） |

### 6-3. smtp-gateway のシミュレーターテスト

smtp-gateway の `POST /simulate` エンドポイントで、実際に送信せずにパイプラインの動作を確認できます。

```bash
curl -s -X POST http://localhost:8080/simulate \
  -H "Content-Type: message/rfc822" \
  --data-binary @tests/e2e/testdata/emls/inbound-normal.eml \
  | jq .

# 正常時: action: "deliver", inspect_results に各ワーカーの結果が含まれる
```

### 6-4. ログ確認

```bash
# Postfix ログ
journalctl -u postfix -f

# Rspamd ログ
journalctl -u rspamd -f

# smtp-gateway ログ（バイナリ直接実行）
./dist/smtp-gateway 2>&1 | jq .

# smtp-gateway ログ（Docker）
docker compose -f docker/docker-compose.yml logs -f smtp-gateway
```

---

## トラブルシューティング

### メールがループする

**症状**: `too many hops` エラー、または Postfix が自分自身に配送しようとする。

**原因**: 再インジェクトポート 10025 で受け取ったメールが再び `content_filter` に引っかかっている。

```bash
# master.cf の 10025 定義に -o content_filter= が含まれているか確認
grep -A 10 "^10025" /etc/postfix/master.cf
```

### SPF / DKIM / DMARC が none になる

**症状**: `Authentication-Results` に `spf=none` や `dkim=none` が含まれる。

確認順序:

1. **Rspamd の起動確認**: `systemctl status rspamd`
2. **milter ポートの確認**: `ss -tlnp | grep 11332`
3. **milter_headers.conf の確認**: `use = ["authentication-results"]` が設定されているか
4. **テスト環境の場合**: 送信元 IP が `local_addrs` に含まれていないか（含まれていると検証がスキップされる）
5. **DKIM の場合**: DNS レコードが正しく登録されているか: `dig TXT dkim._domainkey.example.com +short`

### smtp-gateway が接続を拒否する（550 Unauthorized）

**原因**: `config/mailshield.yaml` の `trusted_sources` に Postfix の IP が含まれていない。

```yaml
server:
  trusted_sources:
    - 127.0.0.1        # Postfix が同一ホストの場合
    - 192.0.2.1        # Postfix が別ホストの場合
```

変更後は smtp-gateway を再起動してください。

### 再インジェクト後のメールが最終宛先に届かない

**確認**: `transport_maps` の配送先ホスト名が `[ ]` で囲まれているか確認します。  
`[ ]` がないと MX を引いてゲートウェイ自身にループします。

```bash
# 正: MX を引かず直接接続
example.com  smtp:[mail.example.com]:25

# 誤: MX を引く（ゲートウェイ自身にループする可能性）
example.com  smtp:mail.example.com:25
```

```bash
postmap /etc/postfix/transport_maps
systemctl reload postfix
```
