/**
 * THROWAWAY design harness — four treatments for a "parked" sidebar row
 * (stopped / evicted) against the shipped one, on identical sample sessions.
 *
 * Not wired to anything: the rows here are a local re-implementation of
 * ThreadRow's markup so a variant can change one visual dimension without
 * putting a `variant` prop on the real component. Delete once a treatment is
 * picked and folded into ThreadRow.
 */
import { createFileRoute } from "@tanstack/react-router";
import { Check, CircleStop, GitMerge, type LucideIcon, Pencil, Unplug } from "lucide-react";
import { cn } from "~/lib/utils";

export const Route = createFileRoute("/dev/parked")({
  component: DevParked,
});

type Rest = "" | "stopped" | "evicted" | "finished" | "merged";

interface Row {
  name: string;
  slug: string;
  initials: string;
  hue: string;
  /** Live third line — present only while the agent is actually working. */
  live?: string;
  rest: Rest;
  time: string;
  /** Filed away by the user. */
  archived?: boolean;
}

const REST_GLYPH: Record<Exclude<Rest, "">, LucideIcon> = {
  stopped: CircleStop,
  evicted: Unplug,
  finished: Check,
  merged: GitMerge,
};

/** One shelf of sessions that exercises every resting outcome at once. */
const ROWS: Row[] = [
  {
    name: "Seat-map race condition",
    slug: "alltix-api",
    initials: "AX",
    hue: "#ff9e64",
    live: "editing derive.ts · 12 tool calls",
    rest: "",
    time: "now",
  },
  {
    name: "Stop button + live context meter",
    slug: "agentique",
    initials: "AG",
    hue: "#7aa2f7",
    rest: "stopped",
    time: "49m",
  },
  {
    name: "Upgrade claudecli-go to v0.3.0",
    slug: "agentkit",
    initials: "AK",
    hue: "#73daca",
    rest: "evicted",
    time: "2d",
  },
  {
    name: "Session UI improvements",
    slug: "agentique",
    initials: "AG",
    hue: "#7aa2f7",
    rest: "finished",
    time: "5h",
  },
  {
    name: "Windows packaging",
    slug: "agentique",
    initials: "AG",
    hue: "#7aa2f7",
    rest: "merged",
    time: "3d",
    archived: true,
  },
];

/** Which rows a variant treats as "still yours" (hue) vs settled (grey). */
type HueRule = (row: Row) => boolean;

const HUE_LIVE_ONLY: HueRule = (r) => !r.rest;
const HUE_UNFINISHED: HueRule = (r) => !r.rest || r.rest === "stopped" || r.rest === "evicted";
const HUE_UNFILED: HueRule = (r) => !r.archived && r.rest !== "merged";

interface VariantSpec {
  key: string;
  title: string;
  blurb: string;
  hue: HueRule;
  /** Glyph beside the rest word on the repo line. */
  glyph: boolean;
  /** Parked rows get a hollow (ring, no fill) project chip. */
  hollowChip?: boolean;
  /** Parked rows spend the right-hand slot on a state pill instead of a time. */
  pill?: boolean;
}

const VARIANTS: VariantSpec[] = [
  {
    key: "now",
    title: "0 · Shipped today",
    blurb:
      "Colour = the CLI is connected. Everything at rest drops to grey, so a session someone killed 40 minutes ago reads exactly like one merged last week.",
    hue: HUE_LIVE_ONLY,
    glyph: false,
  },
  {
    key: "a",
    title: "A · Hue + state glyph",
    blurb:
      "Stopped and evicted keep the full project hue; a glyph on the repo line names why the process isn't running. Grey now means finished or merged.",
    hue: HUE_UNFINISHED,
    glyph: true,
  },
  {
    key: "b",
    title: "B · Hue + hollow chip",
    blurb:
      "Same hue, but the chip loses its fill and becomes a ring. Identity stays at full strength while one small mark still says 'no process behind this'.",
    hue: HUE_UNFINISHED,
    glyph: true,
    hollowChip: true,
  },
  {
    key: "c",
    title: "C · Hue + resume pill",
    blurb:
      "The right-hand slot — the one always-aligned column, where NEW already lives — carries the state. Scannable down a column without reading a word.",
    hue: HUE_UNFINISHED,
    glyph: false,
    pill: true,
  },
  {
    key: "d",
    title: "D · Grey means filed",
    blurb:
      "The reframe: hue tracks 'is this still mine to deal with', not 'is a process alive'. Everything unarchived keeps its colour; only merged and archived go grey.",
    hue: HUE_UNFILED,
    glyph: true,
  },
];

function Chip({ row, hue, hollow }: { row: Row; hue: boolean; hollow: boolean }) {
  return (
    <span
      className={cn(
        "flex size-3.5 shrink-0 items-center justify-center rounded",
        !hue && "bg-border/40 text-muted-foreground",
      )}
      style={
        hue
          ? hollow
            ? { boxShadow: `inset 0 0 0 1px ${row.hue}`, color: row.hue }
            : { backgroundColor: `${row.hue}26`, color: row.hue }
          : undefined
      }
    >
      <span className="text-[7px] font-bold">{row.initials}</span>
    </span>
  );
}

function StatePill({ rest }: { rest: Exclude<Rest, ""> }) {
  const Glyph = REST_GLYPH[rest];
  return (
    <span className="ml-auto flex shrink-0 items-center gap-1 rounded-full border border-border/70 bg-secondary/60 px-1.5 py-px font-mono text-[9.5px] font-semibold uppercase tracking-wider text-muted-foreground">
      <Glyph className="size-2.5" />
      {rest}
    </span>
  );
}

function VariantRow({ row, spec }: { row: Row; spec: VariantSpec }) {
  const hue = spec.hue(row);
  const parked = row.rest === "stopped" || row.rest === "evicted";
  const Glyph = row.rest ? REST_GLYPH[row.rest] : null;
  const pill = spec.pill && parked;

  return (
    <div className="rounded-lg px-2.5 py-1.5 hover:bg-sidebar-accent/60">
      {/* Repo line */}
      <span className="flex items-center gap-1.5">
        <Chip row={row} hue={hue} hollow={!!spec.hollowChip && parked} />
        <span
          className={cn(
            "min-w-0 shrink truncate font-mono text-[10px] font-medium",
            !hue && "text-muted-foreground",
          )}
          style={hue ? { color: row.hue } : undefined}
        >
          {row.slug}
        </span>
        {row.rest && !pill && (
          <span className="flex shrink-0 items-center gap-1 font-mono text-[10px] text-muted-foreground-faint">
            ·{spec.glyph && Glyph ? <Glyph className="size-2.5" /> : null}
            {row.rest}
          </span>
        )}
        {pill && row.rest ? (
          <StatePill rest={row.rest} />
        ) : (
          <span className="ml-auto shrink-0 font-mono text-[10.5px] tabular-nums text-muted-foreground-faint">
            {row.time}
          </span>
        )}
      </span>

      {/* Title line */}
      <span
        className={cn(
          "mt-px block truncate text-[13px] font-medium text-foreground",
          row.rest === "merged" && "text-muted-foreground-faint line-through",
          !hue && row.rest !== "merged" && "text-muted-foreground",
        )}
      >
        {row.name}
      </span>

      {/* State line — live rows only */}
      {row.live && (
        <span className="mt-px flex items-center gap-1.5 text-teal">
          <Pencil className="size-[11px] shrink-0" />
          <span className="min-w-0 flex-1 truncate font-mono text-[10.5px] leading-[1.4]">
            {row.live}
          </span>
        </span>
      )}
    </div>
  );
}

function DevParked() {
  return (
    <div className="h-full overflow-y-auto bg-background p-8">
      <h1 className="text-lg font-semibold text-foreground-bright">
        Parked rows — stopped &amp; evicted
      </h1>
      <p className="mb-6 mt-1 max-w-2xl text-[13px] text-muted-foreground">
        Same five sessions in every column. Watch rows 2 and 3 (stopped, evicted): they are
        unarchived work someone still cares about, and today they read the same as row 5, which is
        merged and filed.
      </p>
      <div className="flex flex-wrap items-start gap-6">
        {VARIANTS.map((spec) => (
          <div key={spec.key} className="w-[300px]">
            <div className="mb-1 text-[12px] font-semibold text-foreground-bright">
              {spec.title}
            </div>
            <p className="mb-2 h-20 text-[11px] leading-[1.45] text-muted-foreground">
              {spec.blurb}
            </p>
            <div className="rounded-lg bg-sidebar p-1">
              {ROWS.map((row) => (
                <VariantRow key={row.name} row={row} spec={spec} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
