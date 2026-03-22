import { Plus } from "lucide-react";
import { Badge } from "~/components/ui/badge";

export function SessionTabs() {
  return (
    <div className="border-b flex items-center gap-1 p-2">
      <button
        type="button"
        className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm bg-accent"
        onClick={() => console.log("Session 1 clicked")}
      >
        Session 1
        <Badge variant="secondary" className="text-xs">
          idle
        </Badge>
      </button>
      <button
        type="button"
        className="flex items-center gap-2 rounded-md px-3 py-1.5 text-sm hover:bg-accent transition-colors"
        onClick={() => console.log("Session 2 clicked")}
      >
        Session 2
        <Badge variant="outline" className="text-xs border-yellow-500 text-yellow-600">
          running
        </Badge>
      </button>
      <button
        type="button"
        className="ml-1 rounded-md p-1.5 hover:bg-accent transition-colors"
        onClick={() => console.log("New session clicked")}
      >
        <Plus className="h-4 w-4" />
      </button>
    </div>
  );
}
