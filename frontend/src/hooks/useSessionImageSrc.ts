import { useEffect, useState } from "react";
import { apiFetch, sessionFileMachineId, sessionFilePath } from "~/lib/machines/api";

/**
 * The src an <img> can load for an image the transcript refers to.
 *
 * Three shapes arrive here: a data URL (a live tool result, or an attachment
 * still in the composer), a session-content path on the primary
 * (`/api/sessions/{id}/files/…` or `/events/…/images/…`, cookie-authenticated
 * and loadable as-is), and the same path for a session on a paired machine,
 * which the browser cannot load directly because that machine wants a bearer.
 * The last is fetched through the machine-aware client into an object URL.
 *
 * Returns null while a remote image is still in flight, and revokes the
 * object URL when the source changes or the component unmounts.
 */
export function useSessionImageSrc(src: string | undefined): string | null {
  const machineId = src ? sessionFileMachineId(src) : undefined;
  const filePath = src ? sessionFilePath(src) : undefined;
  const [blobUrl, setBlobUrl] = useState<string | null>(null);

  useEffect(() => {
    if (!machineId || !filePath) return;
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
  }, [machineId, filePath]);

  if (!src) return null;
  if (!machineId) {
    // A primary session's path: normalize an absolute-localhost variant to
    // the relative form so it loads from any device. Anything unrecognized
    // (a data URL, an external image) passes through untouched.
    return filePath ?? src;
  }
  return blobUrl;
}
