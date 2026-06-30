# ARC 署名統合ガイド

`arcsealer-worker` を有効にすると、処理済みメールに ARC（Authenticated Received Chain）署名を付与できます。

---

## ARC とは

ARC はメール転送時に認証情報が失われる問題を解決するプロトコルです。
MailShield のようなメールゲートウェイがメール本文を変換すると、元の DKIM 署名が無効になります。
ARC を使うと、「私がメールを受け取ったとき DKIM は valid だった」という情報を署名付きで引き継げます。

Exchange Online・Gmail・Google Workspace はいずれも ARC を検証し、正当な ARC シーラーを信頼リストに登録することで誤検知を防ぎます。

---

## 前提条件

- MailShield がドメインを保有していること（例: `mailshield.example.com`）
- そのドメインの DNS TXT レコードを編集できること
- Exchange Online または Google Workspace への管理者アクセス権があること

---

## ステップ 1: 署名鍵を生成する

```bash
cd config/arc
./generate-key.sh
```

実行すると 2 つのファイルが生成されます。

| ファイル | 内容 |
|---------|------|
| `config/arc/private.pem` | RSA 2048 bit 秘密鍵（Git にコミットしない） |
| `config/arc/public.pem` | 対応する公開鍵 |

スクリプトは最後に DNS レコード用の公開鍵 base64 を出力します。

```
=== DNS TXT レコード用の公開鍵（base64） ===
MIIBIjANBgkqhkiG9w0BAQEF...（省略）...DAQAB

TXT レコード名: <selector>._domainkey.<signing_domain>
TXT レコード値: v=DKIM1; k=rsa; p=<上記の base64>
```

---

## ステップ 2: DNS TXT レコードを登録する

`mailshield.yaml` に設定するセレクターとドメインに合わせて TXT レコードを追加します。

| 項目 | 値（例） |
|-----|---------|
| レコード名 | `mailshield._domainkey.example.com` |
| レコード値 | `v=DKIM1; k=rsa; p=<generate-key.sh が出力した base64>` |
| TTL | 3600（1 時間）推奨 |

**セレクターの選択指針**

- 秘密鍵をローテーションするたびに新しいセレクターを使います
- 例: `mailshield-2024`, `mailshield-2025`
- 古いセレクターの TXT レコードはローテーション後も 48 時間は残しておきます

確認コマンド:

```bash
dig TXT mailshield._domainkey.example.com +short
# "v=DKIM1; k=rsa; p=MIIBIjANBgkqhkiG9w0BAQEF..."
```

---

## ステップ 3: mailshield.yaml を設定する

```yaml
workers:
  transform:
    - name: arcsealer-worker
      enabled: true
      order: 6          # 他の変換ワーカーより後ろに配置する
```

`config/workers/conf/arcsealer-worker.yaml` を作成します。

```yaml
# ARC セレクターと署名ドメイン
# TXT レコード名: <selector>._domainkey.<signing_domain>
selector: mailshield
signing_domain: example.com

# 秘密鍵ファイルパス（コンテナ内のパス）
private_key_path: /app/config/arc/private.pem
```

Docker Compose で秘密鍵ファイルをマウントします。

```yaml
# docker-compose.yml（抜粋）
services:
  smtp-gateway:
    volumes:
      - ./config:/app/config        # config/arc/private.pem が含まれる
```

---

## ステップ 4: Exchange Online で Trusted ARC Sealer を登録する

Exchange Online は ARC シーラーを明示的に信頼リストに登録しないと無視します。

### 4-1. PowerShell で登録する

```powershell
Connect-ExchangeOnline

Set-ArcConfig -Identity Default `
  -ArcTrustedSealers "example.com"
```

複数ドメインを登録する場合:

```powershell
Set-ArcConfig -Identity Default `
  -ArcTrustedSealers "example.com","mailshield.example.com"
```

確認:

```powershell
Get-ArcConfig -Identity Default
# ArcTrustedSealers : {example.com}
```

### 4-2. Microsoft 365 管理センターで登録する

1. [Microsoft 365 Defender ポータル](https://security.microsoft.com/) を開く
2. **Email & collaboration** → **Policies & rules** → **Threat policies**
3. **Anti-spam** → **Anti-phishing** ポリシーを開く
4. **Trusted ARC Sealers** セクションで `example.com` を追加する

### 4-3. 動作確認

受信したメールのヘッダーを確認します。

```
Authentication-Results: dmarc=pass header.from=example.com;
  arc=pass (i=1 spf=pass dkim=pass dmarc=pass);
  ...
```

`arc=pass` となっていれば正しく認証されています。

---

## ステップ 5: Google Workspace で Inbound Gateway を設定する

Gmail・Google Workspace は Inbound Gateway 設定でメールゲートウェイを登録します。
登録されたゲートウェイからのメールは ARC を参照して認証を評価します。

### 5-1. Inbound Gateway を登録する

1. [Google 管理コンソール](https://admin.google.com/) を開く
2. **Apps** → **Google Workspace** → **Gmail** → **Spam, Phishing and Malware**
3. **Inbound gateway** セクションを展開し **Configure** をクリックする
4. 以下を設定する

| 設定項目 | 値 |
|---------|---|
| Gateway IPs | MailShield ホストの IP アドレスまたは CIDR |
| Automatically detect external IP | チェックを入れる（ゲートウェイが NAT の場合） |
| Require TLS for connections from the email gateways listed above | 本番環境では有効にすることを推奨 |
| Message is considered spam if it does not pass SPF check | 環境に応じて判断 |
| Disable Gmail spam evaluation on mail from this gateway | MailShield で Rspamd を使う場合はチェックを入れる |

5. **Save** をクリックする

### 5-2. 動作確認

受信したメールのヘッダーを Gmail で確認します（「元のメールを表示」）。

```
Received-SPF: pass (google.com: domain of sender@example.com designates <gateway-ip> as permitted sender)
ARC-Authentication-Results: i=2; mx.google.com;
       dkim=pass header.i=@example.com header.b="...";
       arc=pass (i=1 spf=pass smtp.mailfrom=sender@example.com dkim=pass dkimDomain=example.com);
```

`arc=pass` と表示されれば正しく設定されています。

---

## 鍵のローテーション手順

1. 新しいセレクター（例: `mailshield-2025`）で `generate-key.sh` を実行する
2. 新しいセレクターの DNS TXT レコードを追加する（古いレコードはまだ削除しない）
3. DNS の TTL 時間（通常 1〜24 時間）待つ
4. `arcsealer-worker.yaml` の `selector` を新しい値に更新する
5. smtp-gateway を再起動する（`docker compose -f docker/docker-compose.yml restart smtp-gateway`）
6. 48 時間後に古い TXT レコードを削除する

---

## トラブルシューティング

| 症状 | 確認事項 |
|-----|---------|
| ARC ヘッダーがメールに追加されない | `arcsealer-worker` が `enabled: true` になっているか確認 |
| `arc=fail` になる | DNS TXT レコードが正しく登録されているか `dig` で確認 |
| Exchange Online が ARC を無視する | `Get-ArcConfig` で Trusted ARC Sealers にドメインが含まれているか確認 |
| 秘密鍵が読めないエラー | `private_key_path` のファイルが存在し、コンテナ内で読み取れるか確認 |

ログでのデバッグ:

```bash
docker compose -f docker/docker-compose.yml logs smtp-gateway | grep arcsealer
```

---

## 参考

- [RFC 8617 — The Authenticated Received Chain (ARC) Protocol](https://datatracker.ietf.org/doc/html/rfc8617)
- [Exchange Online での ARC 設定](https://learn.microsoft.com/ja-jp/microsoft-365/security/office-365-security/email-authentication-arc-use)
- [Google Workspace Inbound Gateway](https://support.google.com/a/answer/60730)
