-- subject-virus-inspector
-- 設定ファイル（/app/config/workers/conf/subject-virus-inspector.yaml）の
-- キーワードに一致する件名を検知する検査ワーカー。
--
-- inspect(mail, config) の引数:
--   mail   : メール情報テーブル（subject, from, to, auth_results など）
--   config : subject-virus-inspector.yaml の内容
--
-- 戻り値:
--   { detected: bool, score: int, details: table }

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
