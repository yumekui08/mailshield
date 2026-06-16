# 002: after-queue content filter 方式の採用

## 決定

smtp-inbound は Postfix の after-queue content filter として動作する（port 10025）。

## 理由

- Postfix がメールをキューに受け取った後に処理するため、MTA側のキューイングが保証される
- smtp-inbound が 451 を返すと Postfix がリトライキューに残す（メール消失を防ぐ）
- before-queue filter と違い、Postfix のキュー管理・バウンス処理をそのまま活用できる
