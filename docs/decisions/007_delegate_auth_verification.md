# 007: SPF/DKIM/DMARC 検証は前段（Rspamd）に委任する

## 決定

SPF/DKIM/DMARC の検証は MailShield では実装せず、エッジの Postfix + Rspamd に委任する。
MailShield は Rspamd が付与する `Authentication-Results` ヘッダーを解析して
`auth_results` としてポリシー条件・ワーカーに提供する（現行実装のまま）。

## 理由

- **SPF は after-queue の位置では原理的に検証できない。** SPF 検証には
  「送信元 MTA の接続元 IP」が必要だが、MailShield から見た接続元は常に
  前段 Postfix（localhost）であり、エッジで検証する以外に正しい方法がない
- **DKIM もエッジ受信時点での評価が適切。** 前段・自身の変換処理でヘッダーや本文が
  変更される可能性があり、受信した瞬間に最も近い場所で検証するのが最も信頼できる
- スパム判定エンジン（ベイズ・fuzzy hash・RBL・greylisting 等）の再実装は
  スコープの暴発であり、認証検証だけ内製しても Rspamd 依存は消えない

## 補足

- 送信方向の **ARC シール（arcsealer ワーカー）は MailShield 側に残す。**
  これは変換パイプライン通過後の署名であり、変換の最終段で行う必要があるため
- Rspamd なし構成では `auth_results` は既定値（検証なし）となる。
  認証結果ベースのポリシーを書く場合は前段に Rspamd（または同等の検証 milter）が必須である旨を
  ドキュメントに明記する
