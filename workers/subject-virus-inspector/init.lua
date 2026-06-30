-- subject-virus-inspector
-- config/workers/subject-virus-inspector.yaml のキーワードに一致する件名を検知する。

local M = {}

M.name = "subject-virus-inspector"  -- ドキュメント用（ワーカー名はディレクトリ名が正）
M.type = "inspect"

function M.inspect(mail, config)
    local keywords = config.keywords or { "virus" }
    local score    = config.score    or 100
    local subject  = mail.subject    or ""
    local lower    = string.lower(subject)

    for _, kw in ipairs(keywords) do
        if string.find(lower, kw, 1, true) then
            return {
                detected = true,
                score    = score,
                details  = { reason = "subject contains '" .. kw .. "'" },
            }
        end
    end

    return { detected = false, score = 0, details = {} }
end

return M
