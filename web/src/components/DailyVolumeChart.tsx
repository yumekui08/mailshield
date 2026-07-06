import type { StatsTimeseriesPoint } from "../types";

// 系列色は tailwind.config.js の chart.* と同値（dataviz バリデーター検証済み）
const SERIES = [
  { key: "delivered", label: "配送", color: "#0e8a4f" },
  { key: "quarantined", label: "隔離", color: "#d24343" },
  { key: "rejected", label: "拒否", color: "#3d72ad" },
] as const;

// 軸目盛り用に max を切りのよい数に切り上げる
function niceCeil(n: number): number {
  if (n <= 4) return 4;
  const mag = 10 ** Math.floor(Math.log10(n));
  for (const m of [1, 2, 4, 5, 10]) {
    if (n <= m * mag) return m * mag;
  }
  return 10 * mag;
}

function shortDate(iso: string): string {
  const [, m, d] = iso.split("-");
  return `${Number(m)}/${Number(d)}`;
}

interface DailyVolumeChartProps {
  points: StatsTimeseriesPoint[];
}

// 日別処理件数の積み上げバーチャート。
// SVG を使わず HTML/CSS で構成する（テキストが常に等倍で描画され、レスポンシブが単純になる）。
export function DailyVolumeChart({ points }: DailyVolumeChartProps) {
  const isEmpty = points.every((p) => p.total === 0);
  const yMax = niceCeil(Math.max(...points.map((p) => p.total), 1));
  const tickFractions = [1, 0.75, 0.5, 0.25];
  // ラベルの重なり防止: 7個程度まで間引く
  const labelEvery = Math.max(1, Math.ceil(points.length / 7));

  if (isEmpty) {
    return (
      <div className="flex h-56 items-center justify-center text-sm text-gray-400">
        この期間に処理されたメールはありません
      </div>
    );
  }

  return (
    <div>
      {/* 凡例 */}
      <div className="mb-4 flex items-center gap-4">
        {SERIES.map((s) => (
          <span key={s.key} className="flex items-center gap-1.5 text-xs text-gray-600">
            <span
              className="inline-block h-2.5 w-2.5 rounded-[3px]"
              style={{ backgroundColor: s.color }}
              aria-hidden
            />
            {s.label}
          </span>
        ))}
      </div>

      <div className="flex">
        {/* Y 軸ラベル */}
        <div className="relative mr-2 h-48 w-8 shrink-0 text-right">
          {tickFractions.map((f) => (
            <div
              key={f}
              className="absolute right-0 -translate-y-1/2 text-[11px] tabular-nums text-gray-400"
              style={{ top: `${(1 - f) * 100}%` }}
            >
              {Math.round(yMax * f).toLocaleString()}
            </div>
          ))}
        </div>

        {/* プロット領域 */}
        <div className="relative h-48 flex-1">
          {/* グリッド線（ヘアライン） */}
          {tickFractions.map((f) => (
            <div
              key={f}
              className="absolute inset-x-0 border-t border-gray-100"
              style={{ top: `${(1 - f) * 100}%` }}
              aria-hidden
            />
          ))}
          {/* ベースライン */}
          <div className="absolute inset-x-0 bottom-0 border-t border-gray-300" aria-hidden />

          {/* バー列 */}
          <div className="absolute inset-0 flex items-end">
            {points.map((p, i) => {
              const isRightHalf = i >= points.length / 2;
              return (
                <div
                  key={p.date}
                  className="group relative flex h-full flex-1 items-end justify-center"
                >
                  {/* ホバーの当たり判定は列全体。ホバー時に淡いウォッシュを敷く */}
                  <div className="absolute inset-0 rounded-sm bg-gray-100 opacity-0 transition-opacity group-hover:opacity-60" />

                  {/* 積み上げバー（下から 配送 → 隔離 → 拒否、セグメント間 2px ギャップ） */}
                  <div className="relative flex h-full w-full max-w-[22px] flex-col-reverse gap-[2px] pb-px">
                    {SERIES.map((s, si) => {
                      const value = p[s.key];
                      if (value === 0) return null;
                      const isTop = SERIES.slice(si + 1).every((t) => p[t.key] === 0);
                      return (
                        <div
                          key={s.key}
                          className={isTop ? "rounded-t-[3px]" : ""}
                          style={{
                            backgroundColor: s.color,
                            height: `${(value / yMax) * 100}%`,
                            minHeight: "2px",
                          }}
                        />
                      );
                    })}
                  </div>

                  {/* ツールチップ */}
                  <div
                    className={`pointer-events-none absolute bottom-full z-10 mb-1.5 hidden min-w-[132px] rounded-md border border-gray-200 bg-surface px-3 py-2 text-xs shadow-sm group-hover:block ${
                      isRightHalf ? "right-0" : "left-0"
                    }`}
                  >
                    <div className="mb-1 font-medium text-gray-900">{shortDate(p.date)}</div>
                    {SERIES.map((s) => (
                      <div key={s.key} className="flex items-center justify-between gap-3 leading-5">
                        <span className="flex items-center gap-1.5 text-gray-600">
                          <span
                            className="inline-block h-2 w-2 rounded-[2px]"
                            style={{ backgroundColor: s.color }}
                            aria-hidden
                          />
                          {s.label}
                        </span>
                        <span className="tabular-nums text-gray-900">
                          {p[s.key].toLocaleString()}
                        </span>
                      </div>
                    ))}
                    <div className="mt-1 flex items-center justify-between gap-3 border-t border-gray-100 pt-1 leading-5">
                      <span className="text-gray-600">合計</span>
                      <span className="tabular-nums font-medium text-gray-900">
                        {p.total.toLocaleString()}
                      </span>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      {/* X 軸ラベル */}
      <div className="ml-10 flex">
        {points.map((p, i) => (
          <div key={p.date} className="flex-1 pt-1.5 text-center text-[11px] tabular-nums text-gray-400">
            {i % labelEvery === 0 ? shortDate(p.date) : " "}
          </div>
        ))}
      </div>

      {/* スクリーンリーダー用のデータテーブル */}
      <table className="sr-only">
        <caption>日別メール処理件数</caption>
        <thead>
          <tr>
            <th>日付</th>
            {SERIES.map((s) => (
              <th key={s.key}>{s.label}</th>
            ))}
            <th>合計</th>
          </tr>
        </thead>
        <tbody>
          {points.map((p) => (
            <tr key={p.date}>
              <td>{p.date}</td>
              {SERIES.map((s) => (
                <td key={s.key}>{p[s.key]}</td>
              ))}
              <td>{p.total}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
