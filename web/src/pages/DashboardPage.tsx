import { useNavigate } from "react-router-dom";
import { Paperclip, ArrowRight } from "lucide-react";
import { useStats, useStatsTimeseries } from "../hooks/useStats";
import { useMessageList } from "../hooks/useMessages";
import { Skeleton } from "../components/ui/skeleton";
import { StatusBadge } from "../components/StatusBadge";
import { PageHeader } from "../components/PageHeader";
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
    <div className="rounded-lg border border-gray-200 bg-surface p-4">
      <div className="flex items-center gap-2">
        {accent && (
          <span className="h-2 w-2 rounded-sm" style={{ backgroundColor: accent }} aria-hidden />
        )}
        <div className="text-xs font-medium text-gray-500">{label}</div>
      </div>
      <div className="mt-1.5 text-[26px] font-semibold leading-none tracking-tight text-gray-900">
        {today.toLocaleString()}
      </div>
      <div className="mt-2 text-xs text-gray-400">
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
    <div className="space-y-4 p-6">
      <PageHeader title="ダッシュボード" description="メールゲートウェイの処理状況" />

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
            accent="var(--chart-delivered)"
          />
          <StatTile
            label="隔離"
            today={stats.today.quarantined}
            week={stats.week.quarantined}
            accent="var(--chart-quarantined)"
          />
          <StatTile
            label="拒否"
            today={stats.today.rejected}
            week={stats.week.rejected}
            accent="var(--chart-rejected)"
          />
        </div>
      )}

      {/* 日別推移チャート */}
      <div className="rounded-lg border border-gray-200 bg-surface p-4">
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
        <div className="flex items-center justify-between border-b border-gray-100 px-4 py-3">
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
            <thead className="bg-gray-50">
              <tr className="border-b border-gray-100 text-left">
                <th className="h-8 px-3 text-[11px] font-medium uppercase tracking-wide text-gray-500">
                  受信日時
                </th>
                <th className="h-8 px-3 text-[11px] font-medium uppercase tracking-wide text-gray-500">
                  送信元
                </th>
                <th className="h-8 px-3 text-[11px] font-medium uppercase tracking-wide text-gray-500">
                  件名
                </th>
                <th className="h-8 px-3 text-[11px] font-medium uppercase tracking-wide text-gray-500">
                  ステータス
                </th>
                <th className="h-8 px-3 text-[11px] font-medium uppercase tracking-wide text-gray-500">
                  サイズ
                </th>
              </tr>
            </thead>
            <tbody>
              {recent.data.map((msg) => (
                <tr
                  key={msg.id}
                  className="cursor-pointer border-b border-gray-100 transition-colors last:border-0 hover:bg-gray-50"
                  onClick={() => navigate(`/messages/${msg.id}`)}
                >
                  <td className="whitespace-nowrap px-3 py-2 text-xs tabular-nums text-gray-500">
                    {formatDate(msg.received_at)}
                  </td>
                  <td className="max-w-[160px] truncate px-3 py-2 text-gray-700">
                    {msg.from_address}
                  </td>
                  <td className="max-w-[240px] px-3 py-2">
                    <div className="flex items-center gap-1.5 truncate">
                      {msg.has_attachment && (
                        <Paperclip className="h-3.5 w-3.5 shrink-0 text-gray-400" />
                      )}
                      <span className="truncate text-gray-900">{msg.subject}</span>
                    </div>
                  </td>
                  <td className="whitespace-nowrap px-3 py-2">
                    <StatusBadge status={msg.status} />
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-xs tabular-nums text-gray-500">
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
