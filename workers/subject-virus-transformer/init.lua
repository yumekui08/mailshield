-- subject-virus-transformer
-- 設定ファイル（/app/config/workers/conf/subject-virus-transformer.yaml）の
-- キーワードに一致する件名冒頭にプレフィックスを付加する変換ワーカー。
--
-- transform(mail, config) の引数:
--   mail   : メール情報テーブル（subject フィールドを変更すると EML も書き換えられる）
--   config : subject-virus-transformer.yaml の内容
--
-- 戻り値:
--   変更後の mail テーブル

local M = {}

M.name = "subject-virus-transformer"  -- ドキュメント用（ワーカー名はディレクトリ名が正）
M.type = "transform"

function M.transform(mail, config)
    local keywords = config.keywords or { "virus" }
    local prefix   = config.prefix   or "[迷惑メール注意] "
    local subject  = mail.subject    or ""
    local lower    = string.lower(subject)

    for _, kw in ipairs(keywords) do
        if string.find(lower, kw, 1, true) then
            mail.subject = prefix .. subject
            break
        end
    end

    return mail
end

return M
