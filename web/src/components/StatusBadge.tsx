import { Badge } from "./ui/badge";
import type { MessageStatus } from "../types";

const statusConfig: Record<
  MessageStatus,
  { label: string; variant: "red" | "green" | "slate" | "yellow" | "blue" | "default" }
> = {
  quarantined: { label: "隔離中", variant: "red" },
  delivered: { label: "配送済み", variant: "green" },
  rejected: { label: "拒否", variant: "slate" },
  processing: { label: "処理中", variant: "yellow" },
  received: { label: "受信済み", variant: "blue" },
  approval_pending: { label: "承認待ち", variant: "yellow" },
  expired: { label: "期限切れ", variant: "default" },
};

interface StatusBadgeProps {
  status: MessageStatus;
}

export function StatusBadge({ status }: StatusBadgeProps) {
  const config = statusConfig[status] ?? { label: status, variant: "default" as const };
  return <Badge variant={config.variant}>{config.label}</Badge>;
}
