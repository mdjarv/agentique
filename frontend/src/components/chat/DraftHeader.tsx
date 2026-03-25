import { Plus } from "lucide-react";

export function DraftHeader() {
  return (
    <div className="border-b px-4 py-2 flex items-center gap-2 text-sm shrink-0">
      <Plus className="h-4 w-4 text-muted-foreground" />
      <span className="font-medium text-muted-foreground">New session</span>
    </div>
  );
}
