# Rspamd milter プロキシ設定
# デフォルトは localhost:11332 だが Docker では他コンテナからアクセスできないため
# 全インタフェースにバインドする
milter = yes;
timeout = 120s;

upstream "local" {
  default = yes;
  hosts = "localhost:11333";
}

bind_socket = "*:11332";
