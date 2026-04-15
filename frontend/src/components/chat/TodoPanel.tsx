import { Circle, CircleCheckBig, ListTodo, Loader2, PanelRightClose } from "lucide-react";
import { ANIMATE_DEFAULT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import type { TodoItem } from "~/stores/chat-store";

interface TodoPanelProps {
  todos: TodoItem[];
  onCollapse: () => void;
}

export function StatusIcon({ status }: { status: TodoItem["status"] }) {
  switch (status) {
    case "completed":
      return <CircleCheckBig className="h-3.5 w-3.5 text-success shrink-0" />;
    case "in_progress":
      return <Loader2 className="h-3.5 w-3.5 text-primary animate-spin shrink-0" />;
    case "pending":
      return <Circle className="h-3.5 w-3.5 text-muted-foreground-faint shrink-0" />;
  }
}

export function TodoItemRow({ item }: { item: TodoItem }) {
  const isCompleted = item.status === "completed";
  const isActive = item.status === "in_progress";
  const label = isActive ? (item.activeForm ?? item.content) : item.content;

  return (
    <div className="flex items-start gap-2 py-1.5">
      <div className="mt-0.5">
        <StatusIcon status={item.status} />
      </div>
      <span
        className={`text-xs leading-relaxed ${
          isCompleted
            ? "line-through text-muted-foreground-faint"
            : isActive
              ? "text-foreground"
              : "text-muted-foreground"
        }`}
      >
        {label}
      </span>
    </div>
  );
}

export function TodoPanel({ todos, onCollapse }: TodoPanelProps) {
  const [animateRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);
  const completed = todos.filter((t) => t.status === "completed").length;

  return (
    <div className="w-72 border-l flex flex-col h-full bg-background shrink-0">
      <div className="shrink-0 px-3 py-2 border-b flex items-center gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground shrink-0" />
        <span className="text-xs font-medium">Todos</span>
        <span className="text-xs text-muted-foreground-faint">
          {completed}/{todos.length}
        </span>
        <button
          type="button"
          onClick={onCollapse}
          className="ml-auto p-0.5 rounded hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
        >
          <PanelRightClose className="h-3.5 w-3.5" />
        </button>
      </div>
      <div ref={animateRef} className="flex-1 overflow-y-auto px-3 py-2">
        {todos.map((item) => (
          <TodoItemRow key={`${item.status}-${item.content}`} item={item} />
        ))}
      </div>
    </div>
  );
}

interface CollapsedTodoStripProps {
  todos: TodoItem[];
  onExpand: () => void;
}

export function CollapsedTodoStrip({ todos, onExpand }: CollapsedTodoStripProps) {
  const completed = todos.filter((t) => t.status === "completed").length;

  return (
    <button
      type="button"
      onClick={onExpand}
      className="w-9 border-l flex flex-col items-center py-3 gap-2 shrink-0 hover:bg-muted/50 transition-colors"
    >
      <ListTodo className="h-4 w-4 text-muted-foreground" />
      <span className="text-[10px] text-muted-foreground-faint">
        {completed}/{todos.length}
      </span>
    </button>
  );
}
