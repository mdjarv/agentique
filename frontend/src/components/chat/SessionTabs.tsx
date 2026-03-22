import { Badge } from "~/components/ui/badge";

interface SessionTabsProps {
  state: string;
}

export function SessionTabs({ state }: SessionTabsProps) {
  const badgeVariant = state === "running" ? "outline" : "secondary";
  const badgeClass =
    state === "running"
      ? "border-yellow-500 text-yellow-600"
      : state === "failed"
        ? "border-red-500 text-red-500"
        : "";

  return (
    <div className="border-b flex items-center gap-1 p-2">
      <button
        type="button"
        className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm bg-accent"
      >
        Session 1
        <Badge variant={badgeVariant} className={`text-xs ${badgeClass}`}>
          {state}
        </Badge>
      </button>
    </div>
  );
}
