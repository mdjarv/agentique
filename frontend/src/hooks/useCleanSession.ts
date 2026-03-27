import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { cleanSession } from "~/lib/session-actions";
import { getErrorMessage } from "~/lib/utils";

export function useCleanSession(sessionId: string) {
  const ws = useWebSocket();
  const [cleaning, setCleaning] = useState(false);

  const handleClean = useCallback(async () => {
    setCleaning(true);
    try {
      const r = await cleanSession(ws, sessionId);
      if (r.status === "cleaned") {
        toast.success("Cleaned");
      } else {
        toast.error(r.error);
      }
    } catch (err) {
      toast.error(getErrorMessage(err, "Clean failed"));
    } finally {
      setCleaning(false);
    }
  }, [ws, sessionId]);

  return { cleaning, handleClean };
}
