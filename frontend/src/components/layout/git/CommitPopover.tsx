import { Check, Loader2 } from "lucide-react";
import { type ReactNode, useCallback, useState } from "react";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";

interface CommitPopoverProps {
  onCommit: (message: string) => Promise<void>;
  committing: boolean;
  children: ReactNode;
}

export function CommitPopover({ onCommit, committing, children }: CommitPopoverProps) {
  const [open, setOpen] = useState(false);
  const [message, setMessage] = useState("");

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      const trimmed = message.trim();
      if (!trimmed || committing) return;
      try {
        await onCommit(trimmed);
        setMessage("");
        setOpen(false);
      } catch {
        // Error already toasted by caller — keep popover open
      }
    },
    [message, committing, onCommit],
  );

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>{children}</PopoverTrigger>
      <PopoverContent side="top" align="start" className="w-64 p-2">
        <form onSubmit={handleSubmit} className="flex items-center gap-1.5">
          <Input
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            placeholder="Commit message..."
            className="h-7 text-xs"
            autoFocus
            disabled={committing}
          />
          <Button type="submit" size="icon-xs" disabled={committing || !message.trim()}>
            {committing ? (
              <Loader2 className="size-3 animate-spin" />
            ) : (
              <Check className="size-3" />
            )}
          </Button>
        </form>
      </PopoverContent>
    </Popover>
  );
}
