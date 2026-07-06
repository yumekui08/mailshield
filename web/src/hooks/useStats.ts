import { useQuery } from "@tanstack/react-query";
import { getStats, getStatsTimeseries } from "../lib/api";

export function useStats() {
  return useQuery({
    queryKey: ["stats"],
    queryFn: getStats,
    refetchInterval: 60_000,
  });
}

export function useStatsTimeseries(days: number) {
  return useQuery({
    queryKey: ["stats", "timeseries", days],
    queryFn: () => getStatsTimeseries(days),
    refetchInterval: 60_000,
  });
}
