import { useNavigate } from "react-router-dom";
import { Paperclip, ArrowRight } from "lucide-react";
import { useStats, useStatsTimeseries } from "../hooks/useStats";
import { useMessageList } from "../hooks/useMessages";
import { Skeleton } from "../components/ui/skeleton";
import { StatusBadge } from "../components/StatusBadge";
import { DailyVolumeChart } from "../components/DailyVolumeChart";
import { formatDate, formatBytes } from "../lib/utils";

function StatTile({
  label,
  today,
  week,
  accent,
}: {
  label: string;
  today: number;
  week: number;
  accent?: string;
}) {
  return (
    <div className="rounded-lg border border-gray-200 bg-surface p-5">
      <div className="flex items-center gap-2">
        {accent && (
          <span className="h-2 w-2 rounded-[3px]" style={{ backgroundColor: accent }} aria-hidden />
        )}
        <div className="text-xs font-medium text-gray-500">{label}</div>
      </div>
      <div className="mt-2 text-3xl font-semibold tracking-tight text-gray-900">
        {today.toLocaleString()}
      </div>
      <div className="mt-1 text-xs text-gray-400">
        直近7日間 <span className="tabular-nums text-gray-500">{week.toLocaleString()}</span> 件
      </div>
    </div>
  );
}

function StatsSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
      {[...Array(4)].map((_, i) => (
        <Skeleton key={i} className="h-28 w-full rounded-lg" />
      ))}
    </div>
  );
}

export function DashboardPage() {
  const navigate = useNavigate();
  const { data: stats, isLoading: statsLoading } = useStats();
  const { data: timeseries, isLoading: tsLoading } = useStatsTimeseries(14);
  const { data: recent, isLoading: recentLoading } = useMessageList({
    page: 1,
    per_page: 8,
  });

  return (
    <div className="mx-auto max-w-6xl space-y-6 p-8">
      <div>
        <h1 className="text-xl font-semibold tracking-tight text-gray-900">ダッシュボード</h1>
        <p className="mt-0.5 text-sm text-gray-500">メールゲートウェイの処理状況</p>
      </div>

      {/* 本日の統計タイル */}
      {statsLoading || !stats ? (
        <StatsSkeleton />
      ) : (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <StatTile label="本日の処理" today={stats.today.total} week={stats.week.total} />
          <StatTile
            label="配送"
            today={stats.today.delivered}
            week={stats.week.delivered}
            accent="#0e8a4f"
          />
          <StatTile
            label="隔離"
            today={stats.today.quarantined}
            week={stats.week.quarantined}
            accent="#d24343"
          />
          <StatTile
            label="拒否"
            today={stats.today.rejected}
            week={stats.week.rejected}
            accent="#3d72ad"
          />
        </div>
      )}

      {/* 日別推移チャート */}
      <div className="rounded-lg border border-gray-200 bg-surface p-5">
        <div className="mb-4 flex items-baseline justify-between">
          <h2 className="text-sm font-semibold text-gray-900">処理件数の推移</h2>
          <span className="text-xs text-gray-400">直近14日間・日次</span>
        </div>
        {tsLoading || !timeseries ? (
          <Skeleton className="h-56 w-full" />
        ) : (
          <DailyVolumeChart points={timeseries.data} />
        )}
      </div>

      {/* 直近のメール */}
      <div className="rounded-lg border border-gray-200 bg-surface">
        <div className="flex items-center justify-between border-b border-gray-100 px-5 py-3.5">
          <h2 className="text-sm font-semibold text-gray-900">直近のメール</h2>
          <button
            onClick={() => navigate("/messages")}
            className="flex items-center gap-1 text-xs text-blue-700 transition-colors hover:text-blue-900"
          >
            すべて見る
            <ArrowRight className="h-3 w-3" />
          </button>
        </div>

        {recentLoading ? (
          <div className="space-y-2 p-4">
            {[...Array(5)].map((_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : !recent || recent.data.length === 0 ? (
          <div className="py-12 text-center text-sm text-gray-400">メールがありません</div>
        ) : (
          <table className="w-full text-sm">
            <thead>
              <tr className="text-left">
                <th className="px-5 py-2.5 text-[11px] font-medium uppercase tracking-wide text-gray-400">
                  受信日時
                </th>
                <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wide text-gray-400">
                  送信元
                </th>
                <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wide text-gray-400">
                  件名
                </th>
                <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wide text-gray-400">
                  ステータス
                </th>
                <th className="px-4 py-2.5 text-[11px] font-medium uppercase tracking-wide text-gray-400">
                  サイズ
                </th>
              </tr>
            </thead>
            <tbody>
              {recent.data.map((msg) => (
                <tr
                  key={msg.id}
                  className="cursor-pointer border-t border-gray-100 transition-colors hover:bg-gray-50"
                  onClick={() => navigate(`/messages/${msg.id}`)}
                >
                  <td className="whitespace-nowrap px-5 py-3 text-xs tabular-nums text-gray-500">
                    {formatDate(msg.received_at)}
                  </td>
                  <td className="max-w-[160px] truncate px-4 py-3 text-gray-700">
                    {msg.from_address}
                  </td>
                  <td className="max-w-[240px] px-4 py-3">
                    <div className="flex items-center gap-1.5 truncate">
                      {msg.has_attachment && (
                        <Paperclip className="h-3.5 w-3.5 shrink-0 text-gray-400" />
                      )}
                      <span className="truncate text-gray-900">{msg.subject}</span>
                    </div>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3">
                    <StatusBadge status={msg.status} />
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-xs tabular-nums text-gray-500">
                    {formatBytes(msg.size_bytes)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}
