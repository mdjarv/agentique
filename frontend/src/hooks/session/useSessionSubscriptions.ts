import type { NavigateFn } from "@tanstack/react-router";
import { useApprovalSubscription } from "~/hooks/useApprovalSubscription";
import type { useWebSocket } from "~/hooks/useWebSocket";
import { useSessionEventSubscription } from "./useSessionEventSubscription";
import { useSessionLifecycleSubscription } from "./useSessionLifecycleSubscription";

export function useSessionSubscriptions(ws: ReturnType<typeof useWebSocket>, navigate: NavigateFn) {
  useSessionEventSubscription(ws);
  useSessionLifecycleSubscription(ws, navigate);
  useApprovalSubscription(ws);
}
