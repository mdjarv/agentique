import { useNavigate } from "@tanstack/react-router";
import { Check, ChevronDown, ExternalLink, Loader2, Play, Rocket } from "lucide-react";
import { type ReactNode, createContext, useCallback, useContext, useMemo, useState } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { useWebSocket } from "~/hooks/useWebSocket";
import { type CreateSessionOpts, createSession, submitQuery } from "~/lib/session-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

export interface PromptBlock {
  title: string;
  prompt: string;
  projectSlug?: string;
}

/** Parse title + prompt from a code block's inner text. First line must be `# Title`.
 *  Optional second line `project: <slug>` targets a different project. */
export function parsePromptFromCode(code: string): PromptBlock | null {
  const nl = code.indexOf("\n");
  if (nl === -1) return null;
  const heading = code.slice(0, nl).trim();
  if (!heading.startsWith("# ")) return null;
  const title = heading.slice(2).trim();
  let rest = code.slice(nl + 1);

  let projectSlug: string | undefined;
  const metaMatch = rest.match(/^project:\s*(\S+)\s*\n/);
  if (metaMatch) {
    projectSlug = metaMatch[1];
    rest = rest.slice(metaMatch[0].length);
  }

  const prompt = rest.trim();
  if (!title || !prompt) return null;
  return { title, prompt, projectSlug };
}

/** Extract all prompt blocks from raw markdown content. */
export function parsePromptBlocks(markdown: string): PromptBlock[] {
  const blocks: PromptBlock[] = [];
  const re = /```prompt\s*\n#\s+([^\n]+)\n(?:project:\s*(\S+)\s*\n)?([\s\S]*?)```/g;
  for (const m of markdown.matchAll(re)) {
    const title = m[1]?.trim();
    const projectSlug = m[2]?.trim();
    const prompt = m[3]?.trim();
    if (title && prompt) blocks.push({ title, prompt, projectSlug });
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
  startPrompt: (title: string, prompt: string, targetProjectId?: string) => void;
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
    (title: string, prompt: string, targetProjectId?: string) => {
      const pid = targetProjectId ?? projectId;
      // Deduplicate: skip if a session with this name already exists
      const sessions = useChatStore.getState().sessions;
      const dup = Object.values(sessions).find(
        (s) => s.meta.projectId === pid && s.meta.name === title,
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
          const sid = await createSession(ws, pid, title, true, opts);
          await submitQuery(ws, sid, prompt);
          setCardStates((prev) => ({ ...prev, [title]: { state: "started", sessionId: sid } }));
        } catch (err) {
          const msg = getErrorMessage(err, "Failed to create session");
          toast.error(msg);
          setCardStates((prev) => ({ ...prev, [title]: { state: "error", error: msg } }));
        }
      })();
    },
    [ws, projectId, sessionId],
  );

  const startAll = useCallback(() => {
    const projects = useAppStore.getState().projects;
    for (const p of prompts) {
      const entry = cardStates[p.title];
      if (!entry || entry.state === "idle" || entry.state === "error") {
        const resolvedId = p.projectSlug
          ? projects.find((proj) => proj.slug === p.projectSlug)?.id
          : undefined;
        startPrompt(p.title, p.prompt, resolvedId);
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

export function PromptCard({ title, prompt, projectSlug: slugOverride }: PromptBlock) {
  const ctx = usePromptGroup();
  const navigate = useNavigate();
  const [expanded, setExpanded] = useState(false);

  const contextProjectId = ctx?.projectId ?? "";
  const isStreaming = ctx?.isStreaming ?? false;
  const ctxEntry = ctx?.cardStates[title];

  // Resolve slug override to a project ID; fall back to context project
  const resolvedProject = useAppStore((s) =>
    slugOverride ? s.projects.find((p) => p.slug === slugOverride) : undefined,
  );
  const [overrideProjectId, setOverrideProjectId] = useState<string>();
  const targetProjectId = overrideProjectId ?? resolvedProject?.id ?? contextProjectId;
  const isCrossProject = targetProjectId !== contextProjectId;
  const invalidSlug = slugOverride !== undefined && !resolvedProject && !overrideProjectId;

  const allProjects = useAppStore((s) => s.projects);
  const showProjectPicker = allProjects.length > 1;
  const targetProject = isCrossProject
    ? allProjects.find((p) => p.id === targetProjectId)
    : undefined;

  // Persist "started" across remounts by checking the store for a matching session
  const existingSessionId = useChatStore((s) => {
    if (!targetProjectId) return undefined;
    for (const data of Object.values(s.sessions)) {
      if (data.meta.projectId === targetProjectId && data.meta.name === title) return data.meta.id;
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
    ctx?.startPrompt(title, prompt, isCrossProject ? targetProjectId : undefined);
  }, [ctx, title, prompt, isCrossProject, targetProjectId]);

  const handleStartInProject = useCallback(
    (projectId: string) => {
      setOverrideProjectId(projectId);
      ctx?.startPrompt(title, prompt, projectId);
    },
    [ctx, title, prompt],
  );

  const navSlug = useAppStore((s) => s.projects.find((p) => p.id === targetProjectId)?.slug ?? "");
  const handleNav = useCallback(() => {
    if (sessionId && navSlug) {
      navigate({
        to: "/project/$projectSlug/session/$sessionShortId",
        params: { projectSlug: navSlug, sessionShortId: sessionId.split("-")[0] ?? "" },
      });
    }
  }, [sessionId, navSlug, navigate]);

  const isLong = prompt.split("\n").length > 6 || prompt.length > 400;
  const shown = !isLong || expanded ? prompt : `${prompt.slice(0, 250)}...`;

  return (
    <div className="not-prose my-3 rounded-lg border border-border/40 border-l-[3px] border-l-primary bg-primary/[0.03]">
      <div className="px-4 py-3 space-y-2">
        <div className="flex items-center gap-2">
          <div className="font-medium text-sm">{title}</div>
          {isCrossProject && targetProject && (
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-muted text-muted-foreground font-medium">
              {targetProject.name}
            </span>
          )}
          {invalidSlug && (
            <span className="text-[10px] px-1.5 py-0.5 rounded bg-destructive/10 text-destructive font-medium">
              Unknown project: {slugOverride}
            </span>
          )}
        </div>
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
          {state === "idle" && !showProjectPicker && (
            <Button size="xs" disabled={isStreaming || !ctx || invalidSlug} onClick={handleStart}>
              <Play className="h-3 w-3" />
              Start Session
            </Button>
          )}
          {state === "idle" && showProjectPicker && (
            <DropdownMenu>
              <div
                className={cn(
                  "inline-flex items-center rounded-md text-xs font-medium",
                  "bg-primary text-primary-foreground shadow-xs",
                  (isStreaming || !ctx) && "opacity-50 pointer-events-none",
                )}
              >
                <button
                  type="button"
                  disabled={isStreaming || !ctx || invalidSlug}
                  onClick={handleStart}
                  className="inline-flex items-center gap-1.5 h-6 px-2 rounded-l-md hover:bg-primary-foreground/10 transition-colors disabled:opacity-50 disabled:pointer-events-none"
                >
                  <Play className="h-3 w-3" />
                  Start Session
                </button>
                <div className="w-px h-4 bg-primary-foreground/25" />
                <DropdownMenuTrigger asChild>
                  <button
                    type="button"
                    disabled={isStreaming || !ctx}
                    className="inline-flex items-center h-6 px-1.5 rounded-r-md hover:bg-primary-foreground/10 transition-colors disabled:pointer-events-none"
                    aria-label="Choose project"
                  >
                    <ChevronDown className="h-3 w-3" />
                  </button>
                </DropdownMenuTrigger>
              </div>
              <DropdownMenuContent align="end">
                {allProjects.map((project) => (
                  <DropdownMenuItem
                    key={project.id}
                    onClick={() => handleStartInProject(project.id)}
                    className="text-xs gap-2"
                  >
                    <Check
                      className={cn(
                        "h-3 w-3",
                        project.id === targetProjectId ? "opacity-100" : "opacity-0",
                      )}
                    />
                    {project.name}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuContent>
            </DropdownMenu>
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
