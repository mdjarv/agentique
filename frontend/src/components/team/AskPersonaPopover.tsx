import { MessageCircle } from "lucide-react";
import { useCallback, useRef, useState } from "react";
import { Badge } from "~/components/ui/badge";
import { Button } from "~/components/ui/button";
import { Input } from "~/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import { useWebSocket } from "~/hooks/useWebSocket";
import { askPersona } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";
import { ACTION_VARIANT } from "./InteractionRow";

export function AskPersonaPopover({
  profileId,
  teamId,
  profileName,
}: {
  profileId: string;
  teamId: string;
  profileName: string;
}) {
  const ws = useWebSocket();
  const [open, setOpen] = useState(false);
  const [question, setQuestion] = useState("");
  const [result, setResult] = useState<{
    response: string;
    action: string;
    responseMs: number;
    confidence: number;
  } | null>(null);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const handleAsk = useCallback(async () => {
    if (!question.trim() || loading) return;
    setLoading(true);
    setResult(null);
    setError("");
    try {
      const res = await askPersona(ws, { profileId, teamId, question: question.trim() });
      setResult(res);
    } catch (e) {
      setError(getErrorMessage(e, "Query failed"));
    } finally {
      setLoading(false);
    }
  }, [ws, profileId, teamId, question, loading]);

  const handleOpenChange = useCallback((next: boolean) => {
    setOpen(next);
    if (next) {
      setQuestion("");
      setResult(null);
      setError("");
      setTimeout(() => inputRef.current?.focus(), 50);
    }
  }, []);

  return (
    <Popover open={open} onOpenChange={handleOpenChange}>
      <PopoverTrigger asChild>
        <button type="button" className="text-muted-foreground hover:text-primary">
          <MessageCircle className="size-3" />
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-80" side="left">
        <div className="space-y-2">
          <p className="text-xs font-medium">Ask {profileName}&apos;s persona</p>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              handleAsk();
            }}
            className="flex gap-1.5"
          >
            <Input
              ref={inputRef}
              value={question}
              onChange={(e) => setQuestion(e.target.value)}
              placeholder="Do you handle API routing?"
              className="h-7 text-xs"
              disabled={loading}
            />
            <Button
              type="submit"
              size="sm"
              className="h-7 px-2 text-xs"
              disabled={loading || !question.trim()}
            >
              {loading ? "..." : "Ask"}
            </Button>
          </form>
          {error && (
            <div className="rounded border border-destructive/30 bg-destructive/10 p-2 text-xs text-destructive">
              {error}
            </div>
          )}
          {result && (
            <div className="space-y-1.5">
              <div className="rounded border bg-muted/50 p-2 text-xs whitespace-pre-wrap max-h-48 overflow-y-auto">
                {result.response}
              </div>
              <div className="flex items-center gap-2 text-[10px] text-muted-foreground/60">
                <Badge
                  variant={ACTION_VARIANT[result.action] ?? "secondary"}
                  className="text-[10px] px-1 py-0"
                >
                  {result.action}
                </Badge>
                <span>{result.responseMs}ms</span>
                <span>{(result.confidence * 100).toFixed(0)}%</span>
              </div>
            </div>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
