# 004: MariaDB をデフォルト DB として採用

## 決定

デフォルト DB を MariaDB 11.x とする。将来的には PostgreSQL への切替も可能な設計とする。

## 理由

- 日本企業の既存環境では MySQL/MariaDB が多く、導入摩擦が少ない
- Galera Cluster による高可用性構成が本番環境で実績がある
- MailRepository interface で抽象化しているため、将来の DB 切替はアダプター追加のみで対応可能

## テナント分離の注意点

PostgreSQL の Row Level Security (RLS) が使えないため、アプリケーション層で
全クエリに `WHERE tenant_id = ?` を必ず含めるルールを徹底する。
