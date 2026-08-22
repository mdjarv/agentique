/**
 * Every thread-sidebar row state on one page, in a real 288px rail.
 *
 * The live sidebar only ever shows the handful of states your sessions happen
 * to be in, so the glyph lexicon (and the amber pulse that rides it) can't be
 * eyeballed without one. Presentational only — VMs are hand-built here.
 */
import { createFileRoute } from "@tanstack/react-router";
import { ThreadRow } from "~/components/layout/thread-sidebar/ThreadRow";
import type { ThreadRowVM } from "~/components/layout/thread-sidebar/types";

export const Route = createFileRoute("/dev/rows")({
  component: DevRows,
});

function vm(overrides: Partial<ThreadRowVM>): ThreadRowVM {
  return {
    sessionId: crypto.randomUUID(),
    name: "Upgrade claudecli-go to v0.3.0",
    untitled: false,
    projectSlug: "agentkit",
    projectLabel: "agentkit",
    projectInitials: "AK",
    projectColorBg: "#73daca",
    projectColorFg: "#73daca",
    badge: null,
    awake: true,
    restToken: "",
    timeLabel: "3h",
    struck: false,
    unread: false,
    pinned: false,
    lastActivity: Date.now(),
    ...overrides,
  };
}

/** The row reused for the selected-card sample, so the focused extras show. */
const WORKING_ROW = vm({
  badge: "working",
  workKind: "edit",
  livePhrase: { text: "editing derive.ts · 12 tool calls", tone: "work" },
  todo: { done: 3, total: 7 },
  branch: "session-b30ab203",
  model: "opus[1m]",
  turns: 12,
});

/** One row per work kind — the `working` badge's glyph refinements. */
const WORK_ROWS: { label: string; vm: ThreadRowVM }[] = [
  {
    label: "working · run",
    vm: vm({
      badge: "working",
      workKind: "run",
      livePhrase: { text: "running command · 4 tool calls", tone: "work" },
    }),
  },
  {
    label: "working · read",
    vm: vm({
      badge: "working",
      workKind: "read",
      livePhrase: { text: "reading use-thread-groups.ts", tone: "work" },
    }),
  },
  {
    label: "working · delegate",
    vm: vm({
      badge: "working",
      workKind: "delegate",
      livePhrase: { text: "delegating · 2 tool calls", tone: "work" },
      workers: 2,
    }),
  },
  {
    label: "working · web",
    vm: vm({
      badge: "working",
      workKind: "web",
      livePhrase: { text: "searching web", tone: "work" },
    }),
  },
  {
    label: "working · task",
    vm: vm({
      badge: "working",
      workKind: "task",
      livePhrase: { text: "managing tasks", tone: "work" },
      todo: { done: 5, total: 9 },
    }),
  },
  {
    label: "working · tool (mcp)",
    vm: vm({
      badge: "working",
      workKind: "tool",
      livePhrase: { text: "using tool", tone: "work" },
    }),
  },
  {
    label: "working · configure",
    vm: vm({
      badge: "working",
      workKind: "configure",
      livePhrase: { text: "configuring", tone: "work" },
    }),
  },
];

const ROWS: { label: string; vm: ThreadRowVM }[] = [
  { label: "working · edit — live narration", vm: WORKING_ROW },
  {
    label: "working — no narration yet",
    vm: vm({ badge: "working", livePhrase: { text: "working", tone: "work" } }),
  },
  {
    label: "planning",
    vm: vm({ badge: "planning", livePhrase: { text: "planning", tone: "work" } }),
  },
  {
    label: "attention — tool approval (pulses)",
    vm: vm({
      name: "Seat-map race condition",
      projectSlug: "alltix-api",
      projectInitials: "AX",
      projectColorBg: "#ff9e64",
      projectColorFg: "#ff9e64",
      badge: "attention",
      livePhrase: { text: "go test -race ./ws/...", tone: "attn" },
      timeLabel: "1m",
    }),
  },
  {
    label: "question — open question (pulses)",
    vm: vm({
      name: "Multi Machine Versioning",
      projectSlug: "agentique",
      projectInitials: "AG",
      badge: "question",
      livePhrase: { text: "Which auth method should pairing use?", tone: "attn" },
      timeLabel: "2m",
    }),
  },
  {
    label: "merging",
    vm: vm({
      badge: "merging",
      livePhrase: { text: "merging", tone: "merge" },
      projectColorBg: "#5e9eff",
      projectColorFg: "#5e9eff",
    }),
  },
  {
    label: "failed",
    vm: vm({ badge: "failed", livePhrase: { text: "exit 1", tone: "fail" } }),
  },
  {
    label: "unread completion",
    vm: vm({
      name: "Session UI Improvements",
      projectSlug: "agentique",
      projectInitials: "AG",
      projectColorBg: "#9ece6a",
      projectColorFg: "#9ece6a",
      badge: "unread",
      unread: true,
      livePhrase: { text: "new", tone: "unread" },
      timeLabel: "12m",
      workers: 3,
    }),
  },
  {
    label: "draft",
    vm: vm({ badge: "draft", livePhrase: { text: "draft", tone: "draft" } }),
  },
  {
    label: "at rest — stopped",
    vm: vm({
      name: "Stop button + live context meter",
      projectSlug: "agentique",
      projectInitials: "AG",
      badge: null,
      awake: false,
      restToken: "stopped",
      timeLabel: "49m",
    }),
  },
  {
    label: "at rest — evicted",
    vm: vm({ badge: "off", awake: false, restToken: "evicted", timeLabel: "2d" }),
  },
];

function noop() {}

function DevRows() {
  return (
    <div className="h-full overflow-y-auto bg-background p-8">
      <h1 className="mb-6 text-lg font-semibold text-foreground-bright">Sidebar row states</h1>
      <div className="flex flex-wrap gap-8">
        <div className="flex flex-col gap-3">
          {ROWS.map(({ label, vm: rowVm }) => (
            <div key={label} className="flex flex-col gap-1">
              <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground-faint">
                {label}
              </span>
              <div className="w-72 rounded-lg bg-sidebar p-1">
                <ThreadRow
                  vm={rowVm}
                  selected={false}
                  onClick={noop}
                  onTogglePin={noop}
                  onArchive={noop}
                />
              </div>
            </div>
          ))}
        </div>
        <div className="flex flex-col gap-3">
          <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground-faint">
            work kinds
          </span>
          <div className="w-72 rounded-lg bg-sidebar p-1">
            {WORK_ROWS.map(({ label, vm: rowVm }) => (
              <ThreadRow
                key={label}
                vm={rowVm}
                selected={false}
                onClick={noop}
                onTogglePin={noop}
                onArchive={noop}
              />
            ))}
          </div>
          <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground-faint">
            settled (shelf / archived)
          </span>
          <div className="w-72 rounded-lg bg-sidebar p-1">
            {ROWS.slice(-2).map(({ label, vm: rowVm }) => (
              <ThreadRow
                key={label}
                vm={rowVm}
                selected={false}
                compact
                onClick={noop}
                onTogglePin={noop}
                onArchive={noop}
              />
            ))}
          </div>
          <span className="font-mono text-[10px] uppercase tracking-wider text-muted-foreground-faint">
            selected (focused card)
          </span>
          <div className="w-72 rounded-lg bg-sidebar p-1">
            <ThreadRow
              vm={WORKING_ROW}
              selected
              onClick={noop}
              onTogglePin={noop}
              onArchive={noop}
            />
          </div>
        </div>
      </div>
    </div>
  );
}
