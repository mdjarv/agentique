import { GitBranch, SendHorizonal } from "lucide-react";
import { useRef, useState } from "react";
import { Button } from "~/components/ui/button";
import { cn } from "~/lib/utils";

interface MessageComposerProps {
  onSend: (prompt: string) => void;
  disabled: boolean;
  isDraft?: boolean;
  worktree?: boolean;
  onWorktreeChange?: (value: boolean) => void;
}

export function MessageComposer({
  onSend,
  disabled,
  isDraft,
  worktree,
  onWorktreeChange,
}: MessageComposerProps) {
  const [text, setText] = useState("");
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleSend = () => {
    const trimmed = text.trim();
    if (!trimmed || disabled) return;
    onSend(trimmed);
    setText("");
    if (textareaRef.current) {
      textareaRef.current.style.height = "auto";
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const handleInput = () => {
    const el = textareaRef.current;
    if (el) {
      el.style.height = "auto";
      el.style.height = `${Math.min(el.scrollHeight, 200)}px`;
    }
  };

  return (
    <div className="border-t p-4 space-y-2">
      {isDraft && (
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => onWorktreeChange?.(!worktree)}
            className={cn(
              "flex items-center gap-1.5 text-xs rounded-full px-2.5 py-1 border transition-colors",
              worktree
                ? "bg-primary/10 border-primary/30 text-primary"
                : "bg-muted border-transparent text-muted-foreground hover:border-border",
            )}
          >
            <GitBranch className="h-3 w-3" />
            {worktree ? "Worktree" : "Local"}
          </button>
        </div>
      )}
      <div className="flex gap-3 items-end">
        <textarea
          ref={textareaRef}
          autoFocus
          value={text}
          onChange={(e) => {
            setText(e.target.value);
            handleInput();
          }}
          onKeyDown={handleKeyDown}
          placeholder="Send a message..."
          className="flex-1 resize-none rounded-md border bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          rows={1}
          style={{ maxHeight: "200px" }}
          disabled={disabled}
        />
        <Button size="icon" onClick={handleSend} disabled={disabled || !text.trim()}>
          <SendHorizonal className="h-4 w-4" />
        </Button>
      </div>
    </div>
  );
}
