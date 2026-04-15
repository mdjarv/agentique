import type { ReactNode } from "react";

/**
 * Fixed-width column for icons/indicators in sidebar rows.
 * Matches the 5-unit (20px) indent step so columns align naturally.
 */
export function IconSlot({ children }: { children: ReactNode }) {
  return <span className="w-5 h-5 flex items-center justify-center shrink-0">{children}</span>;
}
