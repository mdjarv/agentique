/**
 * Throws when a REST response is not ok, preferring a `{ error }` message from
 * the JSON body and falling back to `fallback`. The body is only read on the
 * error path, so the caller's success-path `res.json()` / `res.text()` is left
 * untouched.
 */
export async function throwIfNotOk(res: Response, fallback: string): Promise<void> {
  if (res.ok) return;
  const body = await res.json().catch(() => null);
  throw new Error(body?.error ?? fallback);
}
