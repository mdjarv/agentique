import { useCallback, useEffect } from "react";
import { useWebSocket } from "~/hooks/useWebSocket";
import { useChatStore } from "~/stores/chat-store";

export function useChatSession(projectId: string) {
  const ws = useWebSocket();
  const store = useChatStore();

  // biome-ignore lint/correctness/useExhaustiveDependencies: intentionally re-subscribe only when projectId changes
  useEffect(() => {
    store.reset();

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubEvent = ws.subscribe("session.event", (payload: any) => {
      const event = payload.event;
      store.appendEvent({
        id: crypto.randomUUID(),
        type: event.type,
        content: event.content,
        toolId: event.id || event.toolUseId,
        toolName: event.name,
        toolInput: event.input,
        cost: event.costUsd,
        duration: event.duration,
        usage: event.usage,
        stopReason: event.stopReason,
        fatal: event.fatal,
      });

      if (event.type === "result") {
        store.completeTurn();
      }
    });

    // biome-ignore lint/suspicious/noExplicitAny: untyped server push payload
    const unsubState = ws.subscribe("session.state", (payload: any) => {
      store.setSessionState(payload.state);
    });

    return () => {
      unsubEvent();
      unsubState();
    };
  }, [projectId]);

  const sendQuery = useCallback(
    async (prompt: string) => {
      let { sessionId } = useChatStore.getState();

      if (!sessionId) {
        try {
          const result = await ws.request<{ sessionId: string }>(
            "session.create",
            { projectId },
            120000,
          );
          sessionId = result.sessionId;
          store.setSessionId(sessionId);
        } catch (err) {
          console.error("Failed to create session:", err);
          return;
        }
      }

      store.startTurn(prompt);
      try {
        await ws.request("session.query", { sessionId, prompt });
      } catch (err) {
        console.error("Failed to send query:", err);
      }
    },
    [projectId, ws, store],
  );

  return { sendQuery };
}
