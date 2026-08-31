/**
 * Which brain is answering, and how hard it is thinking.
 *
 * Model and effort are one decision with two fields, so they are one control
 * rather than two dropdowns that happen to sit together. The trigger carries
 * the model name and a five-bar meter: a meter reads as a *quantity*, which is
 * what stops the level looking like a second dropdown, and it is the only form
 * where `Max` is visibly different from `XHigh` at 11px. The word survives in
 * the tooltip and in the menu.
 *
 * The two halves are not peers, and the design has to show that without a
 * sentence explaining it. `session.set-model` exists, gated on the runtime's
 * `ModelSwitch` capability; there is no `session.set-effort` anywhere, and the
 * provider did not accept a mid-session change when this was last checked. So
 * inside the menu the models are a list and effort is a **locked ramp** — filled
 * to its stop, no thumb, with one line saying when it was set.
 *
 * `locked` is the only difference between this and the new-session panel's
 * version, which is why both surfaces can finally render one component instead
 * of two dropdowns in one place and a read-only chip in the other. If
 * `session.set-effort` ever lands, the flag flips and no layout changes.
 */
import { Check } from "lucide-react";
import { memo, useMemo } from "react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import {
  EFFORT_COLORS,
  EFFORT_LABELS,
  type EffortLevel,
  RAMP_LEVELS,
} from "~/lib/composer-constants";
import {
  buildModelOptions,
  type ModelId,
  type ProviderId,
  providerForModel,
} from "~/lib/model-catalog";
import { cn } from "~/lib/utils";
import { useProviderStore } from "~/stores/provider-store";

interface BrainControlProps {
  model?: ModelId;
  modelDisplayName?: string;
  onModelChange?: (value: ModelId) => void;
  provider?: ProviderId;
  onProviderChange?: (value: ProviderId) => void;
  effort?: EffortLevel;
  /** Absent means the ramp is drawn locked — see the note above. */
  onEffortChange?: (value: EffortLevel) => void;
  /** Glyph-only model name, for the narrow pane. */
  compact?: boolean;
}

/**
 * The effort meter: five bars, filled to the level, in the level's own colour.
 *
 * `RAMP_LEVELS` runs low to high so the bars can be indexed directly; the
 * dropdown's own list runs the other way and that is deliberate — a menu puts
 * the strongest option first, a ramp climbs.
 */
export function EffortMeter({ effort, className }: { effort?: EffortLevel; className?: string }) {
  const idx = RAMP_LEVELS.indexOf((effort ?? "") as EffortLevel);
  const lit = idx + 1;
  const color = EFFORT_COLORS[(effort ?? "") as EffortLevel] ?? "text-muted-foreground";
  return (
    <span
      className={cn("inline-flex items-end gap-[1.5px] h-2.5 shrink-0", className)}
      aria-hidden="true"
    >
      {RAMP_LEVELS.map((lvl, i) => (
        <span
          key={lvl}
          className={cn(
            "w-[2.5px] rounded-[1px] block",
            i < lit ? cn(color, "bg-current") : "bg-border",
          )}
          style={{ height: `${3 + i * 2}px` }}
        />
      ))}
    </span>
  );
}

export const BrainControl = memo(function BrainControl({
  model,
  modelDisplayName,
  onModelChange,
  provider,
  onProviderChange,
  effort,
  onEffortChange,
  compact = false,
}: BrainControlProps) {
  const catalog = useProviderStore((s) => s.models);
  const { options: modelOptions, providerOf } = useMemo(
    () => buildModelOptions(catalog, provider),
    [catalog, provider],
  );

  if (!model) return null;

  const label = modelDisplayName ?? model;
  const effortLabel = EFFORT_LABELS[(effort ?? "") as EffortLevel];
  const title = `${label} · ${effortLabel} effort${onEffortChange ? "" : " (set when the session was created)"}`;

  // No handler at all: the whole control is a reading. The trigger still shows
  // both halves, because what it reports is the point.
  if (!onModelChange) {
    return (
      <span
        className="flex items-center gap-1.5 text-[11px] max-md:text-xs rounded-md px-2 py-1 text-muted-foreground shrink-0"
        title={title}
      >
        {!compact && <span>{label}</span>}
        <EffortMeter effort={effort} />
      </span>
    );
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        title={title}
        className={cn(
          "flex items-center gap-1.5 text-[11px] max-md:text-xs rounded-md px-2 py-1 max-md:py-1.5 shrink-0",
          "text-muted-foreground transition-colors cursor-pointer",
          "hover:text-foreground hover:bg-muted/80 focus-visible:outline-none",
        )}
      >
        <span className={cn(compact && "sr-only")}>{label}</span>
        <EffortMeter effort={effort} />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="min-w-[14rem]">
        <div className="px-2 pt-1.5 pb-1 text-[10px] uppercase tracking-wide text-muted-foreground/70 select-none">
          Model
        </div>
        {modelOptions.map((opt) => (
          <DropdownMenuItem
            key={opt.value}
            onClick={() => {
              const next = opt.value as ModelId;
              const nextProvider = providerOf(opt.value) ?? providerForModel(next);
              if (nextProvider && nextProvider !== provider) onProviderChange?.(nextProvider);
              onModelChange(next);
            }}
            className="text-xs gap-2"
          >
            <Check className={cn("h-3 w-3", opt.value === model ? "opacity-100" : "opacity-0")} />
            <span>{opt.label}</span>
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <EffortRamp effort={effort} onEffortChange={onEffortChange} />
      </DropdownMenuContent>
    </DropdownMenu>
  );
});

/**
 * Effort as a position on a ramp rather than an item in a list — the fact it
 * reports is "how much", and a list of five words does not say that.
 *
 * Locked is the common case, so it is the one drawn plainly: the fill stops
 * where the level is and nothing invites a drag. The live version is the same
 * geometry with hit targets, so the two cannot drift apart.
 */
function EffortRamp({
  effort,
  onEffortChange,
}: {
  effort?: EffortLevel;
  onEffortChange?: (value: EffortLevel) => void;
}) {
  const current = (effort ?? "") as EffortLevel;
  const idx = RAMP_LEVELS.indexOf(current);
  const pct = RAMP_LEVELS.length > 1 ? (idx / (RAMP_LEVELS.length - 1)) * 100 : 0;
  const locked = !onEffortChange;
  const color = EFFORT_COLORS[current] ?? "text-muted-foreground";

  return (
    <div className="px-2 pt-1.5 pb-2 select-none">
      <div className="flex items-baseline justify-between gap-2 pb-2">
        <span className="text-[10px] uppercase tracking-wide text-muted-foreground/70">Effort</span>
        <span className="text-[10px] text-muted-foreground">
          {locked ? "set at creation" : EFFORT_LABELS[current]}
        </span>
      </div>
      <div className="relative h-1 rounded-full bg-border">
        <div
          className={cn(
            "absolute inset-y-0 left-0 rounded-full",
            locked ? "bg-muted-foreground" : cn(color, "bg-current"),
          )}
          style={{ width: `${pct}%` }}
        />
        <span
          className={cn(
            "absolute -top-1 size-2.5 rounded-full border-2 border-popover",
            locked ? "bg-muted-foreground" : cn(color, "bg-current"),
          )}
          style={{ left: `calc(${pct}% - 5px)` }}
        />
      </div>
      <div className="flex justify-between pt-1.5">
        {RAMP_LEVELS.map((lvl) => {
          const on = lvl === current;
          const text = (
            <span
              className={cn(
                "text-[9px] font-mono",
                on && !locked && color,
                on && locked && "text-foreground",
                !on && "text-muted-foreground/60",
              )}
            >
              {EFFORT_LABELS[lvl]}
            </span>
          );
          if (locked) return <span key={lvl}>{text}</span>;
          return (
            <button
              key={lvl}
              type="button"
              onClick={(e) => {
                e.preventDefault();
                onEffortChange(lvl);
              }}
              className="cursor-pointer hover:brightness-125"
            >
              {text}
            </button>
          );
        })}
      </div>
    </div>
  );
}
