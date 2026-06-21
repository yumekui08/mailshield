import { useState } from "react";
import { FlaskConical, Play, CheckCircle, XCircle, AlertTriangle } from "lucide-react";
import { useMutation } from "@tanstack/react-query";
import { simulatePolicy } from "../lib/api";
import type { SimulateResult } from "../lib/api";
import { Button } from "../components/ui/button";
import { Badge } from "../components/ui/badge";
import { useMe } from "../hooks/useAuth";

const DEFAULT_EML = `From: sender@external.example.com
To: user@example.com
Subject: Hello MailShield
MIME-Version: 1.0
Content-Type: text/plain; charset=utf-8

テストメールです。このメールはシミュレーション用です。
`;

function ActionBadge({ action }: { action: string }) {
  if (action === "deliver") return <Badge variant="green">deliver</Badge>;
  if (action === "quarantine") return <Badge variant="yellow">quarantine</Badge>;
  if (action === "reject") return <Badge variant="red">reject</Badge>;
  if (action === "approval") return <Badge variant="blue">approval</Badge>;
  return <Badge variant="default">{action || "—"}</Badge>;
}

function ScoreBadge({ score }: { score: number }) {
  const color = score >= 80 ? "red" : score >= 40 ? "yellow" : "default";
  return <Badge variant={color as "red" | "yellow" | "default"}>{score}</Badge>;
}

export function SimulatePage() {
  const { data: me } = useMe();
  const [eml, setEml] = useState(DEFAULT_EML);
  const [result, setResult] = useState<SimulateResult | null>(null);

  const simulate = useMutation({
    mutationFn: () => simulatePolicy(eml),
    onSuccess: (data) => setResult(data),
  });

  if (me && me.role === "viewer") {
    return (
      <div className="p-6">
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          この画面は operator / admin のみアクセスできます。
        </div>
      </div>
    );
  }

  return (
    <div className="p-6 space-y-6 max-w-4xl">
      <div className="flex items-center gap-2">
        <FlaskConical className="h-5 w-5 text-slate-600" />
        <h1 className="text-xl font-semibold text-slate-900">ポリシーシミュレーター</h1>
      </div>
      <p className="text-sm text-slate-600">
        EML を貼り付けて、現在のワーカー設定・ポリシーでどのように処理されるか確認できます。
        実際の配送・保存は行いません。
      </p>

      {/* EML 入力エリア */}
      <div className="space-y-2">
        <label className="text-sm font-medium text-slate-700">EML（生メール）</label>
        <textarea
          className="w-full h-64 rounded-md border border-slate-300 bg-white p-3 font-mono text-xs text-slate-800 focus:outline-none focus:ring-2 focus:ring-blue-500 resize-y"
          value={eml}
          onChange={(e) => setEml(e.target.value)}
          placeholder="ここに EML を貼り付けてください..."
          spellCheck={false}
        />
      </div>

      <Button
        onClick={() => simulate.mutate()}
        disabled={simulate.isPending || !eml.trim()}
        className="flex items-center gap-2"
      >
        <Play className="h-4 w-4" />
        {simulate.isPending ? "処理中..." : "シミュレーション実行"}
      </Button>

      {simulate.isError && (
        <div className="rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700">
          エラー: {(simulate.error as Error)?.message ?? "不明なエラー"}
        </div>
      )}

      {/* 結果 */}
      {result && (
        <div className="space-y-4 rounded-lg border border-slate-200 bg-slate-50 p-5">
          <h2 className="text-sm font-semibold text-slate-700">シミュレーション結果</h2>

          {/* サマリー */}
          <div className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm">
            <div className="text-slate-500">ルート</div>
            <div className="font-medium text-slate-800">
              {result.route_name} <span className="text-slate-400 text-xs">({result.direction})</span>
            </div>

            <div className="text-slate-500">アクション</div>
            <div><ActionBadge action={result.action} /></div>

            <div className="text-slate-500">適用ルール</div>
            <div className="font-medium text-slate-800">{result.matched_rule || "—"}</div>

            {result.subject_changed && (
              <>
                <div className="text-slate-500">件名（変換前）</div>
                <div className="text-slate-800 line-through">{result.original_subject}</div>
                <div className="text-slate-500">件名（変換後）</div>
                <div className="font-medium text-blue-700">{result.transformed_subject}</div>
              </>
            )}

            <div className="text-slate-500">処理時間</div>
            <div className="text-slate-600">{result.processing_ms} ms</div>
          </div>

          {/* 検査ワーカー結果 */}
          {result.inspect_results && result.inspect_results.length > 0 && (
            <div className="space-y-2">
              <h3 className="text-xs font-semibold text-slate-500 uppercase tracking-wide">
                検査ワーカー結果
              </h3>
              <div className="space-y-1">
                {result.inspect_results.map((r) => (
                  <div
                    key={r.worker}
                    className="flex items-center gap-3 rounded-md bg-white border border-slate-200 px-3 py-2 text-sm"
                  >
                    {r.detected ? (
                      <AlertTriangle className="h-4 w-4 text-yellow-500 shrink-0" />
                    ) : (
                      <CheckCircle className="h-4 w-4 text-green-500 shrink-0" />
                    )}
                    <span className="font-mono text-xs text-slate-700 w-48 shrink-0">{r.worker}</span>
                    <ScoreBadge score={r.score} />
                    {r.detected && (
                      <span className="text-xs text-yellow-700 font-medium">検知</span>
                    )}
                    {Object.entries(r.details).length > 0 && (
                      <span className="text-xs text-slate-500 truncate">
                        {Object.entries(r.details)
                          .map(([k, v]) => `${k}: ${v}`)
                          .join(", ")}
                      </span>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {(!result.inspect_results || result.inspect_results.length === 0) && (
            <p className="text-sm text-slate-500">有効な検査ワーカーがありません。</p>
          )}
        </div>
      )}
    </div>
  );
}
