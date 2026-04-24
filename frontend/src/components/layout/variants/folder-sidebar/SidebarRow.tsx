import { forwardRef } from "react";
import { cn } from "~/lib/utils";
import { TodoProgressBar } from "./TodoProgressBar";
import type { TodoProgress } from "./types";
import { indentClass } from "./types";

export type BorderState = "running" | "approval" | "failed" | "none";

const BORDER_CLASS: Record<BorderState, string> = {
  running: "bg-current text-teal animate-[border-pulse_2s_ease-in-out_infinite]",
  approval: "bg-current text-orange animate-[border-pulse_1.5s_ease-in-out_infinite]",
  failed: "bg-current text-destructive shadow-[0_0_6px_1px_currentColor]",
  none: "",
};

interface SidebarRowProps extends React.HTMLAttributes<HTMLElement> {
  indent: number;
  selected?: boolean;
  compact?: boolean;
  as?: "button" | "div";
  /** Suppress hover/selected background highlight. */
  plain?: boolean;
  todoProgress?: TodoProgress;
  borderState?: BorderState;
  children: React.ReactNode;
}

export const SidebarRow = forwardRef<HTMLElement, SidebarRowProps>(function SidebarRow(
  {
    indent,
    selected,
    compact,
    as = "button",
    plain,
    todoProgress,
    borderState = "none",
    className,
    children,
    ...props
  },
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
      {/* Left state border */}
      {borderState !== "none" && (
        <div
          className={cn(
            "absolute left-0 top-1 bottom-1 w-[3px] rounded-full",
            BORDER_CLASS[borderState],
          )}
        />
      )}
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
