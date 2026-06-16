import { useNavigate } from "react-router-dom";
import {
  CheckCircle2,
  ShieldAlert,
  XCircle,
  Mail,
  Paperclip,
} from "lucide-react";
import { useStats } from "../hooks/useStats";
import { useMessageList } from "../hooks/useMessages";
import { Skeleton } from "../components/ui/skeleton";
import { StatusBadge } from "../components/StatusBadge";
import { formatDate, formatBytes } from "../lib/utils";
import type { StatsPeriod } from "../types";

function StatCard({
  label,
  value,
  icon: Icon,
  color,
}: {
  label: string;
  value: number;
  icon: React.ElementType;
  color: string;
}) {
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4">
      <div className="flex items-center gap-3">
        <div className={`rounded-md p-2 ${color}`}>
          <Icon className="h-5 w-5 text-white" />
        </div>
        <div>
          <div className="text-2xl font-bold text-gray-900">{value.toLocaleString()}</div>
          <div className="text-xs text-gray-500">{label}</div>
        </div>
      </div>
    </div>
  );
}

function PeriodStats({ period, title }: { period: StatsPeriod; title: string }) {
  return (
    <div>
      <h2 className="text-sm font-semibold text-gray-500 uppercase tracking-wide mb-3">{title}</h2>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <StatCard label="配送済み" value={period.delivered} icon={CheckCircle2} color="bg-green-500" />
        <StatCard label="隔離中" value={period.quarantined} icon={ShieldAlert} color="bg-red-500" />
        <StatCard label="拒否" value={period.rejected} icon={XCircle} color="bg-gray-500" />
        <StatCard label="合計" value={period.total} icon={Mail} color="bg-blue-500" />
      </div>
    </div>
  );
}

function StatsSkeleton() {
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      {[...Array(4)].map((_, i) => (
        <Skeleton key={i} className="h-20 w-full rounded-lg" />
      ))}
    </div>
  );
}

export function DashboardPage() {
  const navigate = useNavigate();
  const { data: stats, isLoading: statsLoading } = useStats();
  const { data: recent, isLoading: recentLoading } = useMessageList({
    page: 1,
    per_page: 8,
  });

  return (
    <div className="p-6 space-y-8">
      <h1 className="text-xl font-semibold text-gray-900">ダッシュボード</h1>

      {/* 当日統計 */}
      {statsLoading ? (
        <div>
          <Skeleton className="h-4 w-24 mb-3" />
          <StatsSkeleton />
        </div>
      ) : stats ? (
        <PeriodStats period={stats.today} title="本日" />
      ) : null}

      {/* 週間統計 */}
      {statsLoading ? (
        <div>
          <Skeleton className="h-4 w-24 mb-3" />
          <StatsSkeleton />
        </div>
      ) : stats ? (
        <PeriodStats period={stats.week} title="直近7日間" />
      ) : null}

      {/* 直近のメール */}
      <div>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-sm font-semibold text-gray-500 uppercase tracking-wide">
            直近のメール
          </h2>
          <button
            onClick={() => navigate("/messages")}
            className="text-xs text-blue-600 hover:text-blue-800 hover:underline"
          >
            すべて見る →
          </button>
        </div>

        <div className="rounded-lg border border-gray-200 bg-white overflow-hidden">
          {recentLoading ? (
            <div className="p-4 space-y-2">
              {[...Array(5)].map((_, i) => <Skeleton key={i} className="h-10 w-full" />)}
            </div>
          ) : !recent || recent.data.length === 0 ? (
            <div className="text-center text-gray-400 py-10 text-sm">
              メールがありません
            </div>
          ) : (
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-100 text-left">
                  <th className="px-4 py-3 text-xs font-medium text-gray-500">受信日時</th>
                  <th className="px-4 py-3 text-xs font-medium text-gray-500">送信元</th>
                  <th className="px-4 py-3 text-xs font-medium text-gray-500">件名</th>
                  <th className="px-4 py-3 text-xs font-medium text-gray-500">ステータス</th>
                  <th className="px-4 py-3 text-xs font-medium text-gray-500">サイズ</th>
                </tr>
              </thead>
              <tbody>
                {recent.data.map((msg) => (
                  <tr
                    key={msg.id}
                    className="border-b border-gray-50 last:border-0 hover:bg-gray-50 cursor-pointer"
                    onClick={() => navigate(`/messages/${msg.id}`)}
                  >
                    <td className="px-4 py-3 text-xs text-gray-500 whitespace-nowrap">
                      {formatDate(msg.received_at)}
                    </td>
                    <td className="px-4 py-3 max-w-[160px] truncate text-gray-700">
                      {msg.from_address}
                    </td>
                    <td className="px-4 py-3 max-w-[240px]">
                      <div className="flex items-center gap-1.5 truncate">
                        {msg.has_attachment && (
                          <Paperclip className="h-3.5 w-3.5 text-gray-400 shrink-0" />
                        )}
                        <span className="truncate text-gray-900">{msg.subject}</span>
                      </div>
                    </td>
                    <td className="px-4 py-3 whitespace-nowrap">
                      <StatusBadge status={msg.status} />
                    </td>
                    <td className="px-4 py-3 text-xs text-gray-500 whitespace-nowrap">
                      {formatBytes(msg.size_bytes)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </div>
  );
}
