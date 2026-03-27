import type { z } from "zod";

/**
 * Validates a payload against a Zod schema. Logs a warning on failure.
 * In test environments (Vitest), set VITE_MSW_STRICT=true to throw instead.
 */
export function validatePayload<T>(schema: z.ZodType<T>, payload: unknown, context: string): T {
  const result = schema.safeParse(payload);
  if (!result.success) {
    const msg = `[MSW Contract] ${context}: ${result.error.message}`;
    if (import.meta.env?.VITE_MSW_STRICT === "true") {
      throw new Error(msg);
    }
    console.warn(msg);
    return payload as T;
  }
  return result.data;
}
