import { useNavigate } from "@tanstack/react-router";
import { Check, ChevronDown, ExternalLink, Loader2, Play, Users2 } from "lucide-react";
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
import { type SwarmMemberSpec, createSwarm } from "~/lib/team-actions";
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

// ---------------------------------------------------------------------------
// State-machine prompt block finder (handles nested code fences)
// ---------------------------------------------------------------------------

export interface RawPromptBlock {
  startLine: number;
  endLine: number;
  content: string;
  fenceLen: number;
  maxInnerFence: number;
}

const RE_PROMPT_OPEN = /^ {0,3}(`{3,})prompt\s*$/;
const RE_BARE_FENCE = /^ {0,3}(`{3,})\s*$/;
const RE_INFO_FENCE = /^ {0,3}(`{3,})\S/;

/** Lookahead: determine whether a bare fence should open an inner code block
 *  rather than close the prompt block.
 *
 *  Counts remaining bare and info fences (stopping at the next prompt opener).
 *  If there are >= 2 unpaired bare fences ahead, this one opens an inner block
 *  (one to close it, one more to close the prompt). */
function shouldOpenInnerBlock(lines: string[], currentIndex: number): boolean {
  let bare = 0;
  let info = 0;
  for (let j = currentIndex + 1; j < lines.length; j++) {
    const line = lines[j] ?? "";
    if (RE_PROMPT_OPEN.test(line)) break;
    if (RE_BARE_FENCE.test(line)) bare++;
    else if (RE_INFO_FENCE.test(line)) info++;
  }
  return bare - info >= 2;
}

/** Find prompt blocks in raw markdown, correctly handling nested code fences.
 *  Tracks inner code blocks with a boolean flag instead of a depth counter,
 *  using lookahead to distinguish bare fences that open inner blocks from
 *  bare fences that close the prompt. */
export function findRawPromptBlocks(markdown: string): RawPromptBlock[] {
  const lines = markdown.split("\n");
  const blocks: RawPromptBlock[] = [];
  let i = 0;

  while (i < lines.length) {
    const cur = lines[i] ?? "";
    const openMatch = RE_PROMPT_OPEN.exec(cur);
    if (!openMatch?.[1]) {
      i++;
      continue;
    }

    const fenceLen = openMatch[1].length;
    const startLine = i;
    const contentLines: string[] = [];
    let maxInnerFence = 0;
    let insideInner = false;
    let innerFenceLen = 0;

    i++;
    let found = false;
    while (i < lines.length) {
      const line = lines[i] ?? "";
      const bareMatch = RE_BARE_FENCE.exec(line);
      const infoMatch = RE_INFO_FENCE.exec(line);

      if (insideInner) {
        if (bareMatch?.[1] && bareMatch[1].length >= innerFenceLen) {
          insideInner = false;
          maxInnerFence = Math.max(maxInnerFence, bareMatch[1].length);
        }
        contentLines.push(line);
      } else if (bareMatch?.[1]) {
        if (shouldOpenInnerBlock(lines, i)) {
          insideInner = true;
          innerFenceLen = bareMatch[1].length;
          maxInnerFence = Math.max(maxInnerFence, bareMatch[1].length);
          contentLines.push(line);
        } else {
          found = true;
          i++;
          break;
        }
      } else if (infoMatch?.[1]) {
        insideInner = true;
        innerFenceLen = infoMatch[1].length;
        maxInnerFence = Math.max(maxInnerFence, infoMatch[1].length);
        contentLines.push(line);
      } else {
        contentLines.push(line);
      }

      i++;
    }

    if (found) {
      blocks.push({
        startLine,
        endLine: i - 1,
        content: contentLines.join("\n"),
        fenceLen,
        maxInnerFence,
      });
    }
  }

  return blocks;
}

/** Extract all prompt blocks from raw markdown content. */
export function parsePromptBlocks(markdown: string): PromptBlock[] {
  return findRawPromptBlocks(markdown)
    .map((raw) => parsePromptFromCode(raw.content))
    .filter((b): b is PromptBlock => b !== null);
}

// ---------------------------------------------------------------------------
// Content segmentation — splits markdown into text + prompt segments so
// prompt blocks never pass through the markdown parser.
// ---------------------------------------------------------------------------

export type ContentSegment =
  | { type: "markdown"; content: string }
  | { type: "prompt"; block: PromptBlock };

/** Split markdown into interleaved text/prompt segments.
 *  Prompt blocks are extracted by our state machine parser and never
 *  reach the markdown renderer, eliminating all fence-nesting issues. */
export function splitByPromptBlocks(markdown: string): ContentSegment[] {
  const rawBlocks = findRawPromptBlocks(markdown);
  if (rawBlocks.length === 0) return [{ type: "markdown", content: markdown }];

  const lines = markdown.split("\n");
  const segments: ContentSegment[] = [];
  let cursor = 0;

  for (const raw of rawBlocks) {
    if (raw.startLine > cursor) {
      const text = lines.slice(cursor, raw.startLine).join("\n");
      if (text.trim()) segments.push({ type: "markdown", content: text });
    }

    const parsed = parsePromptFromCode(raw.content);
    if (parsed) segments.push({ type: "prompt", block: parsed });

    cursor = raw.endLine + 1;
  }

  if (cursor < lines.length) {
    const text = lines.slice(cursor).join("\n");
    if (text.trim()) segments.push({ type: "markdown", content: text });
  }

  return segments;
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
        autoApproveMode: parent?.autoApproveMode,
        effort: parent?.effort,
        behaviorPresets: parent?.behaviorPresets,
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
    const sessions = useChatStore.getState().sessions;
    const parent = sessions[sessionId]?.meta;

    // Partition: same-project prompts go through createSwarm, cross-project through startPrompt.
    const swarmEligible: PromptBlock[] = [];
    const crossProject: { block: PromptBlock; targetId: string }[] = [];

    for (const p of prompts) {
      const entry = cardStates[p.title];
      if (entry && entry.state !== "idle" && entry.state !== "error") continue;

      if (p.projectSlug) {
        const resolved = projects.find((proj) => proj.slug === p.projectSlug);
        if (resolved && resolved.id !== projectId) {
          crossProject.push({ block: p, targetId: resolved.id });
          continue;
        }
      }
      swarmEligible.push(p);
    }

    // Cross-project prompts: individual sessions (can't join same team)
    for (const { block, targetId } of crossProject) {
      startPrompt(block.title, block.prompt, targetId);
    }

    // Same-project prompts: create as a team via createSwarm
    if (swarmEligible.length > 0) {
      const members: SwarmMemberSpec[] = swarmEligible.map((p) => ({
        name: p.title,
        prompt: p.prompt,
        model: parent?.model,
        autoApproveMode: parent?.autoApproveMode,
        effort: parent?.effort,
        behaviorPresets: parent?.behaviorPresets,
      }));

      for (const p of swarmEligible) {
        setCardStates((prev) => ({ ...prev, [p.title]: { state: "creating" } }));
      }

      const teamName = parent?.name ? `${parent.name} team` : "Swarm";

      void (async () => {
        try {
          const result = await createSwarm(ws, projectId, teamName, members, sessionId);
          for (let i = 0; i < swarmEligible.length; i++) {
            const title = swarmEligible[i]?.title;
            if (!title) continue;
            const sid = result.sessionIds[i];
            if (sid) {
              setCardStates((prev) => ({ ...prev, [title]: { state: "started", sessionId: sid } }));
            } else {
              setCardStates((prev) => ({
                ...prev,
                [title]: { state: "error", error: "Failed to create session" },
              }));
            }
          }
          if (result.errors?.length) {
            toast.warning(`Team created with ${result.errors.length} warning(s)`);
          } else {
            toast.success(`Team "${teamName}" created with ${swarmEligible.length} sessions`);
          }
        } catch (err) {
          const msg = getErrorMessage(err, "Failed to create team");
          toast.error(msg);
          for (const p of swarmEligible) {
            setCardStates((prev) => ({ ...prev, [p.title]: { state: "error", error: msg } }));
          }
        }
      })();
    }
  }, [ws, projectId, sessionId, prompts, cardStates, startPrompt]);

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
                <Users2 className="h-3 w-3" />
                Start Team ({prompts.length})
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
