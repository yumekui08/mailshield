import { useQuery } from "@tanstack/react-query";
import { getAuditLogs } from "../lib/api";
import type { AuditLogParams } from "../types";

export function useAuditLogs(params: AuditLogParams = {}) {
  return useQuery({
    queryKey: ["audit-logs", params],
    queryFn: () => getAuditLogs(params),
  });
}
