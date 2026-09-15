import { Link } from "@tanstack/react-router";
import { BookMarked, Check, CircleSlash, Flag, Loader2, ThumbsUp, Wrench, X } from "lucide-react";
import { memo, useMemo } from "react";
import { useSessionLabel } from "~/components/assistant/use-session-label";
import { useFactVerdict } from "~/components/brain/use-fact-verdict";
import { CollapsibleGroup } from "~/components/chat/CollapsibleGroup";
import { ThinkingBlock } from "~/components/chat/ThinkingBlock";
import { ThinkingIcon } from "~/components/chat/ToolIcons";
import { Popover, PopoverContent, PopoverTrigger } from "~/components/ui/popover";
import {
  isMemoryVerb,
  runningStep,
  STEP_KIND_THOUGHT,
  STEP_STATUS_FAILED,
  STEP_STATUS_REFUSED,
  STEP_STATUS_RUNNING,
  stepDuration,
  stepsTitle,
} from "~/lib/assistant/steps";
import type { AssistantStep, AssistantStepFact } from "~/lib/assistant/wire";
import { scopeLabel } from "~/lib/brain-labels";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useFeatureStore } from "~/stores/feature-store";

/**
 * What the assistant did during a turn: the verbs it called and the reasoning
 * it did, folded into one line above the reply (docs/assistant.md, "Steps").
 *
 * It is the session transcript's activity line on purpose — the same
 * `CollapsibleGroup`, the same "N steps, M thoughts" count, the same
 * `ThinkingBlock` for a thought — because a turn's working has to read the same
 * wherever you meet it. Collapsed by default: the reply is what was asked for,
 * and the working is there for when the reply is wrong.
 *
 * While the turn runs (`live`) the collapsed line names the verb it is waiting
 * on, which is what replaced "Thinking…" as the one thing a slow turn shows.
 *
 * Everything a step quotes — an argument, a refusal, a recalled fact — is the
 * model's or the store's text, rendered as text. Nothing here acts on it; the
 * two buttons on a fact are the operator's verdict, sent as their own request.
 */
export const TurnSteps = memo(function TurnSteps({
  steps,
  omitted = 0,
  live = false,
}: {
  steps: AssistantStep[];
  omitted?: number;
  live?: boolean;
}) {
  const running = live ? runningStep(steps) : undefined;
  return (
    <CollapsibleGroup
      title={stepsTitle(steps, omitted)}
      icon={<Wrench className="h-3 w-3" />}
      defaultExpanded={false}
      activeHeader={running ? <RunningHeader step={running} /> : undefined}
      trailingIcons={
        <span className="flex flex-row-reverse items-center gap-1.5 overflow-hidden">
          {steps
            .slice()
            .reverse()
            .map((step) => (
              <StepIcon key={step.seq} step={step} />
            ))}
        </span>
      }
    >
      {steps.map((step) =>
        step.kind === STEP_KIND_THOUGHT ? (
          <ThinkingBlock
            key={step.seq}
            content={step.text ?? ""}
            // The block reads "encrypted" from a signature with no content;
            // the step says so in a flag rather than carrying the signature.
            signature={step.encrypted ? "encrypted" : undefined}
          />
        ) : (
          <VerbStep key={step.seq} step={step} />
        ),
      )}
      {omitted > 0 && (
        <p className="px-2 text-[11px] text-muted-foreground-faint">
          {omitted} more {omitted === 1 ? "step" : "steps"} not kept
        </p>
      )}
    </CollapsibleGroup>
  );
});

function StepIcon({ step }: { step: AssistantStep }) {
  if (step.kind === STEP_KIND_THOUGHT) return <ThinkingIcon className="shrink-0" />;
  const Glyph = isMemoryVerb(step.verb) ? BookMarked : Wrench;
  return (
    <Glyph
      className={cn(
        "h-3 w-3 shrink-0",
        step.status === STEP_STATUS_FAILED && "text-destructive/70",
      )}
    />
  );
}

/** The collapsed line while a verb is out: what the turn is waiting on. */
function RunningHeader({ step }: { step: AssistantStep }) {
  const session = useSessionLabel(step.sessionId);
  const detail = step.detail || session;
  return (
    <>
      <Loader2 className="h-3 w-3 shrink-0 animate-spin text-agent" />
      <span className="shrink-0 font-mono text-info">{step.verb}</span>
      {detail && <span className="min-w-0 truncate text-muted-foreground">{detail}</span>}
    </>
  );
}

/**
 * One verb: its status, its name, what it was asked, what it answered.
 *
 * A refusal or a failure prints its sentence on a line of its own, because
 * that sentence is the most useful thing on the row when a reply went wrong.
 * Refused is not red: a refusal is the table's rules working. Failed is the X,
 * which means "it failed" on every surface.
 */
function VerbStep({ step }: { step: AssistantStep }) {
  const session = useSessionLabel(step.sessionId);
  const detail = step.detail || session;
  const refused = step.status === STEP_STATUS_REFUSED;
  const failed = step.status === STEP_STATUS_FAILED;
  const duration = stepDuration(step.durationMs);
  const summary = refused || failed ? "" : step.outcome;

  return (
    <div className="rounded-md border bg-muted/50 px-2.5 py-1.5 text-xs">
      <div className="flex min-w-0 items-center gap-2">
        <StatusGlyph status={step.status} />
        <span className="shrink-0 font-mono text-info">{step.verb}</span>
        {detail && (
          <span className="min-w-0 truncate text-foreground" title={detail}>
            {detail}
          </span>
        )}
        {step.detail && session && (
          <span className="min-w-0 shrink truncate text-muted-foreground-faint">{session}</span>
        )}
        {(summary || duration) && (
          <span className="ml-auto shrink-0 font-mono text-[10px] tabular-nums text-muted-foreground-faint">
            {[summary, duration].filter(Boolean).join(" · ")}
          </span>
        )}
      </div>
      {(refused || failed) && step.outcome && (
        <p
          className={cn(
            "mt-1 pl-5 leading-snug [overflow-wrap:anywhere]",
            failed ? "text-destructive/80" : "text-muted-foreground",
          )}
        >
          {step.outcome}
        </p>
      )}
      {step.facts && step.facts.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-1 pl-5">
          {step.facts.map((fact) => (
            <FactChip key={fact.id} fact={fact} />
          ))}
        </div>
      )}
    </div>
  );
}

function StatusGlyph({ status }: { status: string | undefined }) {
  switch (status) {
    case STEP_STATUS_RUNNING:
      return <Loader2 className="h-3 w-3 shrink-0 animate-spin text-agent" aria-label="Running" />;
    case STEP_STATUS_REFUSED:
      return (
        <CircleSlash className="h-3 w-3 shrink-0 text-muted-foreground" aria-label="Refused" />
      );
    case STEP_STATUS_FAILED:
      return <X className="h-3 w-3 shrink-0 text-destructive" aria-label="Failed" />;
    default:
      return <Check className="h-3 w-3 shrink-0 text-muted-foreground-faint" aria-label="Done" />;
  }
}

/** Source of a fact written by an agent about a repository nobody here authored. */
const REPORTED_SOURCE = "reported";

/**
 * One fact a recall returned, and the operator's verdict on it.
 *
 * A chip that opens rather than a row, because a recall can return six and the
 * row is one line of a folded list. The pair is the same Helpful / Outdated the
 * old recall card offered, through the same hook, and it is the conversational
 * outcome signal made clickable: "yes, that is right" without typing it.
 */
function FactChip({ fact }: { fact: AssistantStepFact }) {
  const projects = useAppStore((s) => s.projects);
  const brainEnabled = useFeatureStore((s) => s.features.brain);
  const label = useMemo(() => {
    if (!fact.scope) return "";
    return scopeLabel(fact.scope, (id) => projects.find((p) => p.id === id)?.name);
  }, [fact.scope, projects]);
  const { status, busy, act } = useFactVerdict(fact.id);
  const reported = fact.source === REPORTED_SOURCE;

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          className={cn(
            "max-w-full truncate rounded bg-muted px-1.5 py-0.5 text-left text-[11px] text-foreground transition-colors hover:bg-muted-foreground/20",
            status === "helpful" && "ring-1 ring-success/40",
            status === "flagged" && "ring-1 ring-destructive/40",
            reported && "italic",
          )}
          title={fact.text}
        >
          {/* On a phone the label would be most of the chip; the popover says it. */}
          {label && <span className="mr-1 text-muted-foreground-faint max-md:hidden">{label}</span>}
          {fact.text}
        </button>
      </PopoverTrigger>
      <PopoverContent className="w-80 max-w-[calc(100vw-2rem)] space-y-2 p-3">
        <p className="text-[13px] leading-relaxed text-foreground [overflow-wrap:anywhere]">
          {reported ? <q>{fact.text}</q> : fact.text}
        </p>
        {reported && (
          <p className="text-[11px] text-muted-foreground">
            Written by an agent about repository content, so it is a quotation rather than a checked
            fact.
          </p>
        )}
        {label && <p className="font-mono text-[10px] text-muted-foreground-faint">{label}</p>}
        <div className="flex flex-wrap items-center gap-1.5">
          {status === "helpful" ? (
            <span className="flex items-center gap-1 text-[11px] text-success">
              <Check className="h-3 w-3" /> Helpful
            </span>
          ) : status === "flagged" ? (
            <span className="flex items-center gap-1 text-[11px] text-destructive">
              <Flag className="h-3 w-3" /> Flagged
            </span>
          ) : (
            <>
              <button
                type="button"
                disabled={busy}
                onClick={() => act("helpful")}
                className="flex items-center gap-1 rounded border px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-success/10 hover:text-success disabled:opacity-50"
              >
                {status === "confirming" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <ThumbsUp className="h-3 w-3" />
                )}
                Helpful
              </button>
              <button
                type="button"
                disabled={busy}
                onClick={() => act("flag")}
                className="flex items-center gap-1 rounded border px-2 py-0.5 text-[11px] text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
              >
                {status === "flagging" ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <Flag className="h-3 w-3" />
                )}
                Outdated
              </button>
            </>
          )}
          {brainEnabled && (
            <Link
              to="/assistant/memory"
              className="ml-auto text-[11px] text-muted-foreground hover:text-foreground"
            >
              Open Memory
            </Link>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}
