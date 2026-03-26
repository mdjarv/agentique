import { ListPlus, X } from "lucide-react";
import type { QueuedMessage } from "~/stores/chat-store";

interface MessageQueueProps {
  messages: QueuedMessage[];
  onCancel: (message: QueuedMessage) => void;
}

export function MessageQueue({ messages, onCancel }: MessageQueueProps) {
  return (
    <div className="mx-4 mb-2 rounded-md border border-muted-foreground/20 bg-muted/50 px-3 py-2">
      <div className="flex items-center gap-1.5 text-xs text-muted-foreground mb-1.5">
        <ListPlus className="h-3 w-3" />
        <span className="font-medium">Queued ({messages.length})</span>
      </div>
      <div className="space-y-1">
        {messages.map((msg) => (
          <div key={msg.id} className="flex items-center gap-2 text-xs text-foreground/80 group">
            <span className="flex-1 truncate">{msg.prompt}</span>
            <button
              type="button"
              onClick={() => onCancel(msg)}
              className="shrink-0 h-4 w-4 rounded-sm flex items-center justify-center max-md:opacity-100 opacity-0 group-hover:opacity-100 hover:bg-destructive/20 hover:text-destructive transition-opacity"
            >
              <X className="h-3 w-3" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
