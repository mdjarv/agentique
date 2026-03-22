import { SendHorizonal } from "lucide-react";
import { Button } from "~/components/ui/button";

export function MessageComposer() {
  return (
    <div className="border-t p-4 flex gap-3 items-end">
      <textarea
        placeholder="Send a message..."
        className="flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        rows={1}
        style={{ maxHeight: "200px" }}
        disabled
      />
      <Button size="icon" disabled>
        <SendHorizonal className="h-4 w-4" />
      </Button>
    </div>
  );
}
