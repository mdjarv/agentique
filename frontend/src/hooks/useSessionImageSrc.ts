import { useCallback, useEffect, useState } from "react";
import { apiFetch, sessionFileMachineId, sessionFilePath } from "~/lib/machines/api";

export interface SessionImageSrc {
  /** What the <img> loads; null while a fallback fetch is in flight. */
  src: string | null;
  /** Pass to the <img>: a relayed image that fails gets one direct try. */
  onError?: () => void;
}

/**
 * The src an <img> can load for an image the transcript refers to.
 *
 * Two shapes arrive here: a data URL (a live tool result, or an attachment
 * still in the composer), and a session-content path
 * (`/api/sessions/{id}/files/…` or `/events/…/images/…`). A path loads as-is,
 * relative to this page, for every session wherever it runs: the server that
 * served the page relays a paired machine's content (docs/multi-machine.md,
 * "Session files"). An absolute-localhost variant an agent wrote is normalised
 * to that relative form.
 *
 * The one exception is a paired machine on a release from before the relay,
 * which the server answers 501 for. The <img> cannot see the status, so any
 * failure on a remote session's path falls back once to fetching from that
 * machine directly with its bearer into an object URL — which works only where
 * this browser can reach the machine. Contract once no paired release
 * predates the relay.
 */
export function useSessionImageSrc(src: string | undefined): SessionImageSrc {
  const filePath = src ? sessionFilePath(src) : undefined;
  const machineId = src ? sessionFileMachineId(src) : undefined;
  // Which path fell back, so a new source starts on the relay again.
  const [directFor, setDirectFor] = useState<string | null>(null);
  const direct = filePath !== undefined && directFor === filePath;
  const [blobUrl, setBlobUrl] = useState<string | null>(null);

  useEffect(() => {
    if (!direct || !machineId || !filePath) return;
    let cancelled = false;
    let objectUrl: string | null = null;
    apiFetch(machineId, filePath)
      .then((res) => (res.ok ? res.blob() : Promise.reject(new Error(`${res.status}`))))
      .then((blob) => {
        if (cancelled) return;
        objectUrl = URL.createObjectURL(blob);
        setBlobUrl(objectUrl);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
      setBlobUrl(null);
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [direct, machineId, filePath]);

  const onError = useCallback(() => setDirectFor(filePath ?? null), [filePath]);

  if (!src) return { src: null };
  if (!filePath) return { src };
  if (direct) return { src: blobUrl };
  return { src: filePath, onError: machineId ? onError : undefined };
}
