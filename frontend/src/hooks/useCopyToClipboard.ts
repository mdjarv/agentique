import { useCallback, useEffect, useRef, useState } from "react";
import { copyToClipboard } from "~/lib/utils";

const DEFAULT_TIMEOUT = 1500;

export function useCopyToClipboard(timeoutMs = DEFAULT_TIMEOUT) {
  const [copied, setCopied] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>(null);

  useEffect(
    () => () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    },
    [],
  );

  const copy = useCallback(
    (text: string) => {
      copyToClipboard(text).then(() => {
        if (timerRef.current) clearTimeout(timerRef.current);
        setCopied(true);
        timerRef.current = setTimeout(() => setCopied(false), timeoutMs);
      });
    },
    [timeoutMs],
  );

  return { copied, copy } as const;
}
