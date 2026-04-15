import { useCallback, useState } from "react";
import { toast } from "sonner";
import { useWebSocket } from "~/hooks/useWebSocket";
import { createPR } from "~/lib/session/actions";
import { getErrorMessage } from "~/lib/utils";

export function useCreatePR(sessionId: string) {
  const ws = useWebSocket();
  const [creatingPR, setCreatingPR] = useState(false);

  const handlePRSubmit = useCallback(
    async (title: string, body: string) => {
      setCreatingPR(true);
      try {
        const result = await createPR(ws, sessionId, title, body);
        if (result.status === "created" || result.status === "existing") {
          toast.success(`PR ${result.status}: ${result.url}`);
          return true;
        }
        toast.error(result.error);
        return false;
      } catch (err) {
        toast.error(getErrorMessage(err, "PR creation failed"));
        return false;
      } finally {
        setCreatingPR(false);
      }
    },
    [ws, sessionId],
  );

  return { creatingPR, handlePRSubmit };
}
