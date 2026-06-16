import { useQuery } from "@tanstack/react-query";
import { getMessageList, getMessageDetail } from "../lib/api";
import type { MessageListParams } from "../lib/api";

export function useMessageList(params: MessageListParams) {
  return useQuery({
    queryKey: ["messages", params],
    queryFn: () => getMessageList(params),
  });
}

export function useMessageDetail(id: string) {
  return useQuery({
    queryKey: ["messages", id],
    queryFn: () => getMessageDetail(id),
    enabled: !!id,
  });
}
