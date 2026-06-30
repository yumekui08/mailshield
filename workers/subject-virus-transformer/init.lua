-- subject-virus-transformer
-- config/workers/subject-virus-transformer.yaml のキーワードに一致する件名にプレフィックスを付加する。

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
