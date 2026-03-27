import { useNavigate } from "@tanstack/react-router";
import { Check, ExternalLink, Loader2, Play, Rocket } from "lucide-react";
import { type ReactNode, createContext, useCallback, useContext, useMemo, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type CreateSessionOpts, createSession, submitQuery } from "~/lib/session-actions";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

export interface PromptBlock {
  title: string;
  prompt: string;
}

/** Parse title + prompt from a code block's inner text. First line must be `# Title`. */
export function parsePromptFromCode(code: string): PromptBlock | null {
  const nl = code.indexOf("\n");
  if (nl === -1) return null;
  const heading = code.slice(0, nl).trim();
  if (!heading.startsWith("# ")) return null;
  const title = heading.slice(2).trim();
  const prompt = code.slice(nl + 1).trim();
  if (!title || !prompt) return null;
  return { title, prompt };
}

/** Extract all prompt blocks from raw markdown content. */
export function parsePromptBlocks(markdown: string): PromptBlock[] {
  const blocks: PromptBlock[] = [];
  const re = /```prompt\s*\n#\s+([^\n]+)\n([\s\S]*?)```/g;
  for (const m of markdown.matchAll(re)) {
    const title = m[1]?.trim();
    const prompt = m[2]?.trim();
    if (title && prompt) blocks.push({ title, prompt });
  }
  return blocks;
}

// ---------------------------------------------------------------------------
// Context
// ---------------------------------------------------------------------------

type CardState = "idle" | "creating" | "started" | "error";

interface CardEntry {
  state: CardState;
  sessionId?: string;
  error?: string;
}

interface PromptGroupCtx {
  projectId: string;
  sessionId: string;
  isStreaming: boolean;
  cardStates: Record<string, CardEntry>;
  startPrompt: (title: string, prompt: string) => void;
}

const Ctx = createContext<PromptGroupCtx | null>(null);

export function usePromptGroup(): PromptGroupCtx | null {
  return useContext(Ctx);
}

// ---------------------------------------------------------------------------
// Provider — wraps a TextSegment, renders "Start All" footer when 2+ prompts
// ---------------------------------------------------------------------------

export function PromptGroupProvider({
  content,
  projectId,
  sessionId,
  isStreaming,
  children,
}: {
  content: string;
  projectId: string;
  sessionId: string;
  isStreaming: boolean;
  children: ReactNode;
}) {
  const ws = useWebSocket();
  const [cardStates, setCardStates] = useState<Record<string, CardEntry>>({});
  const prompts = useMemo(() => parsePromptBlocks(content), [content]);

  const startPrompt = useCallback(
    (title: string, prompt: string) => {
      // Deduplicate: skip if a session with this name already exists
      const sessions = useChatStore.getState().sessions;
      const dup = Object.values(sessions).find(
        (s) => s.meta.projectId === projectId && s.meta.name === title,
      );
      if (dup) {
        setCardStates((prev) => ({
          ...prev,
          [title]: { state: "started", sessionId: dup.meta.id },
        }));
        return;
      }

      // Inherit settings from parent session
      const parent = sessions[sessionId]?.meta;
      const opts: CreateSessionOpts = {
        planMode: false,
        model: parent?.model,
        autoApprove: parent?.autoApprove,
        effort: parent?.effort,
      };

      setCardStates((prev) => ({ ...prev, [title]: { state: "creating" } }));
      void (async () => {
        try {
          const sid = await createSession(ws, projectId, title, true, opts);
          await submitQuery(ws, sid, prompt);
          setCardStates((prev) => ({ ...prev, [title]: { state: "started", sessionId: sid } }));
        } catch (err) {
          const msg = err instanceof Error ? err.message : "Failed to create session";
          toast.error(msg);
          setCardStates((prev) => ({ ...prev, [title]: { state: "error", error: msg } }));
        }
      })();
    },
    [ws, projectId, sessionId],
  );

  const startAll = useCallback(() => {
    for (const p of prompts) {
      const entry = cardStates[p.title];
      if (!entry || entry.state === "idle" || entry.state === "error") {
        startPrompt(p.title, p.prompt);
      }
    }
  }, [prompts, cardStates, startPrompt]);

  const value = useMemo<PromptGroupCtx>(
    () => ({ projectId, sessionId, isStreaming, cardStates, startPrompt }),
    [projectId, sessionId, isStreaming, cardStates, startPrompt],
  );

  const anyCreating = prompts.some((p) => cardStates[p.title]?.state === "creating");
  const allStarted =
    prompts.length > 0 && prompts.every((p) => cardStates[p.title]?.state === "started");

  return (
    <Ctx.Provider value={value}>
      {children}
      {prompts.length >= 2 && !isStreaming && (
        <div className="flex justify-end mt-2">
          <Button
            size="xs"
            variant={allStarted ? "ghost" : "default"}
            disabled={anyCreating || allStarted}
            onClick={startAll}
          >
            {anyCreating ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin" />
                Starting...
              </>
            ) : allStarted ? (
              <>
                <Check className="h-3 w-3" />
                All Started
              </>
            ) : (
              <>
                <Rocket className="h-3 w-3" />
                Start All ({prompts.length})
              </>
            )}
          </Button>
        </div>
      )}
    </Ctx.Provider>
  );
}

// ---------------------------------------------------------------------------
// Card
// ---------------------------------------------------------------------------

export function PromptCard({ title, prompt }: PromptBlock) {
  const ctx = usePromptGroup();
  const navigate = useNavigate();
  const [expanded, setExpanded] = useState(false);

  const projectId = ctx?.projectId ?? "";
  const isStreaming = ctx?.isStreaming ?? false;
  const ctxEntry = ctx?.cardStates[title];

  // Persist "started" across remounts by checking the store for a matching session
  const existingSessionId = useChatStore((s) => {
    if (!projectId) return undefined;
    for (const data of Object.values(s.sessions)) {
      if (data.meta.projectId === projectId && data.meta.name === title) return data.meta.id;
    }
    return undefined;
  });

  const state: CardState =
    ctxEntry?.state === "creating"
      ? "creating"
      : ctxEntry?.state === "error"
        ? "error"
        : existingSessionId || ctxEntry?.state === "started"
          ? "started"
          : "idle";

  const sessionId = ctxEntry?.sessionId ?? existingSessionId;

  const handleStart = useCallback(() => {
    ctx?.startPrompt(title, prompt);
  }, [ctx, title, prompt]);

  const projectSlug = useAppStore((s) => s.projects.find((p) => p.id === projectId)?.slug ?? "");
  const handleNav = useCallback(() => {
    if (sessionId && projectSlug) {
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug, sessionShortId: sessionId.split("-")[0] ?? "" },
      });
    }
  }, [sessionId, projectSlug, navigate]);

  const isLong = prompt.split("\n").length > 6 || prompt.length > 400;
  const shown = !isLong || expanded ? prompt : `${prompt.slice(0, 250)}...`;

  return (
    <div className="not-prose my-3 rounded-lg border border-border/40 border-l-[3px] border-l-primary bg-primary/[0.03]">
      <div className="px-4 py-3 space-y-2">
        <div className="font-medium text-sm">{title}</div>
        <div className="text-xs text-muted-foreground/80 leading-relaxed whitespace-pre-wrap">
          {shown}
        </div>
        {isLong && (
          <button
            type="button"
            onClick={() => setExpanded((v) => !v)}
            className="text-xs text-primary/70 hover:text-primary transition-colors"
          >
            {expanded ? "Show less" : "Show more"}
          </button>
        )}
        <div className="flex items-center justify-end gap-2 pt-0.5">
          {state === "idle" && (
            <Button size="xs" disabled={isStreaming || !ctx} onClick={handleStart}>
              <Play className="h-3 w-3" />
              Start Session
            </Button>
          )}
          {state === "creating" && (
            <Button size="xs" disabled>
              <Loader2 className="h-3 w-3 animate-spin" />
              Creating...
            </Button>
          )}
          {state === "started" && (
            <Button size="xs" variant="ghost" onClick={handleNav}>
              <ExternalLink className="h-3 w-3" />
              Open Session
            </Button>
          )}
          {state === "error" && (
            <Button size="xs" variant="outline" onClick={handleStart}>
              Retry
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
