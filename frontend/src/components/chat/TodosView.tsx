import { ANIMATE_DEFAULT, useAutoAnimate } from "~/hooks/useAutoAnimate";
import type { TodoItem } from "~/stores/chat-store";
import { TodoItemRow } from "./TodoPanel";

interface TodosViewProps {
  todos: TodoItem[];
}

export function TodosView({ todos }: TodosViewProps) {
  const [animateRef] = useAutoAnimate<HTMLDivElement>(ANIMATE_DEFAULT);
  const completed = todos.filter((t) => t.status === "completed").length;
  const pct = todos.length > 0 ? (completed / todos.length) * 100 : 0;

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="px-4 py-3 border-b space-y-2">
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>
            {completed}/{todos.length} completed
          </span>
          <span className="tabular-nums">{Math.round(pct)}%</span>
        </div>
        <div className="h-1 rounded-full bg-muted overflow-hidden">
          <div
            className="h-full rounded-full bg-success transition-all duration-300"
            style={{ width: `${pct}%` }}
          />
        </div>
      </div>
      <div ref={animateRef} className="px-4 py-2">
        {todos.map((item) => (
          <TodoItemRow key={item.content} item={item} />
        ))}
      </div>
    </div>
  );
}
