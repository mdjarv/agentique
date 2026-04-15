import { forwardRef } from "react";
import { cn } from "~/lib/utils";
import { TodoProgressBar } from "./TodoProgressBar";
import type { TodoProgress } from "./types";
import { indentClass } from "./types";

interface SidebarRowProps extends React.HTMLAttributes<HTMLElement> {
  indent: number;
  selected?: boolean;
  compact?: boolean;
  as?: "button" | "div";
  /** Suppress hover/selected background highlight. */
  plain?: boolean;
  todoProgress?: TodoProgress;
  children: React.ReactNode;
}

export const SidebarRow = forwardRef<HTMLElement, SidebarRowProps>(function SidebarRow(
  { indent, selected, compact, as = "button", plain, todoProgress, className, children, ...props },
  ref,
) {
  const Tag = as;
  const hasTodos = todoProgress && todoProgress.total > 0;

  return (
    <Tag
      ref={ref as React.Ref<HTMLButtonElement & HTMLDivElement>}
      type={as === "button" ? "button" : undefined}
      className={cn(
        "relative flex items-center w-full pr-2 text-left text-xs rounded-sm transition-colors cursor-pointer",
        indentClass(indent),
        compact ? "py-1" : "py-1.5",
        "group/row",
        className,
      )}
      {...props}
    >
      {/* Hover/selected highlight */}
      {!plain && (
        <div
          className={cn(
            "absolute inset-0 rounded-md transition-colors",
            selected ? "sidebar-row-selected" : "sidebar-row-hover",
          )}
        />
      )}
      {/* Content layer — z-[1] to paint above the absolute bg overlay */}
      <span className="relative z-[1] flex items-center flex-1 min-w-0">{children}</span>

      {hasTodos && <TodoProgressBar indent={indent} progress={todoProgress} />}
    </Tag>
  );
});
