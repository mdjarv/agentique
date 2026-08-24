/**
 * One provider CLI, under the machine that runs it (docs/upgrades.md V5b).
 *
 * The row states what this machine would spawn for its next session, how that
 * install got there, and — where it is not ours to touch — the command that
 * would update it. There is deliberately no "update available" and no button:
 * nothing in the stack can compute a verdict yet, and a badge that says
 * "up to date" because nobody looked is worse than no badge (C15).
 *
 * Two things earn prominence over the version number. A second copy on PATH
 * means the version shown has stopped describing the binary that runs, and a
 * disagreement between what was detected and what a session actually reported
 * means detection is describing the wrong binary outright.
 */
import { Check, Copy } from "lucide-react";
import { useState } from "react";
import type { UpdateCLIStatus } from "~/lib/generated-types";
import { cn, copyToClipboard } from "~/lib/utils";

/** How the install is managed, in the user's terms rather than the enum's.
 *  Never branch on `method` for behaviour — the two provider libraries define
 *  their values differently on purpose. This is a label, and only a label. */
function methodLabel(cli: UpdateCLIStatus): string {
  const parts = [cli.method];
  if (cli.versionManager) parts.push(`via ${cli.versionManager}`);
  return parts.join(" · ");
}

function CopyCommand({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        void copyToClipboard(command).then(() => {
          setCopied(true);
          setTimeout(() => setCopied(false), 1500);
        });
      }}
      title="Copy"
      className="group inline-flex min-w-0 max-w-full items-center gap-1.5 rounded border border-border/60 bg-secondary/40 px-1.5 py-0.5 text-left font-mono text-[10.5px] text-muted-foreground transition-colors hover:bg-secondary hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-ring"
    >
      <span className="truncate">{command}</span>
      {copied ? (
        <Check className="size-3 shrink-0 text-foreground" />
      ) : (
        <Copy className="size-3 shrink-0 opacity-60 group-hover:opacity-100" />
      )}
    </button>
  );
}

export function CLIRow({ cli }: { cli: UpdateCLIStatus }) {
  // A version the CLI could not report is not "0" or "unknown version" — say
  // that we could not read it, since the binary is still there and still runs.
  const version = cli.installed || "version unreadable";
  // Detection describes what would be spawned next; lastRan is what actually
  // started. They differ legitimately while an updated CLI has not been picked
  // up yet, so this is worth showing but never an error.
  const drifted = cli.lastRan && cli.installed && cli.lastRan !== cli.installed;

  return (
    <div className="flex flex-col gap-1 py-1.5 pl-3">
      <div className="flex min-w-0 items-baseline gap-2">
        <span className="w-12 shrink-0 text-[11px] font-medium text-foreground">{cli.tool}</span>
        <span className="truncate font-mono text-[11px] text-muted-foreground">{version}</span>
        <span className="truncate text-[11px] text-muted-foreground-faint">{methodLabel(cli)}</span>
      </div>

      {/* What updating it looks like. Self-managed installs say so and stop —
          the tool handles itself, and V5c will put the button here. */}
      <div className="flex min-w-0 flex-col gap-1 pl-14">
        {cli.selfManaged ? (
          <span className="text-[11px] text-muted-foreground-faint">
            updates itself{cli.updateCmd ? ` · ${cli.updateCmd}` : ""}
          </span>
        ) : cli.updateCmd ? (
          <CopyCommand command={cli.updateCmd} />
        ) : (
          <span className="text-[11px] text-muted-foreground-faint">
            no update command is known for this install — update it the way it was installed
          </span>
        )}

        {drifted ? (
          <span className="text-[11px] text-amber-600 dark:text-amber-500">
            your last session ran {cli.lastRan}
          </span>
        ) : null}

        {cli.warnings?.map((w) => (
          <span key={w} className={cn("text-[11px] text-amber-600 dark:text-amber-500")}>
            {w}
          </span>
        ))}
      </div>
    </div>
  );
}
