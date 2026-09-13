/** Settings › Sessions — where new sessions start. Device-local, in the UI store. */
import { SettingsRow, SettingsSection } from "~/components/settings/SettingsLayout";
import { WORKTREE_GLYPH, type WorktreeKind } from "~/lib/session/location";
import { cn } from "~/lib/utils";
import { useUIStore } from "~/stores/ui-store";

// Git's own words (docs: "Where a session's code lives"), never "local" — that
// word means the machine.
const KINDS: { id: WorktreeKind; label: string }[] = [
  { id: "linked", label: "Linked worktree" },
  { id: "main", label: "Main worktree" },
];

export function SessionsSettings() {
  const kind = useUIStore((s) => s.newSessionWorktree);
  const setKind = useUIStore((s) => s.setNewSessionWorktree);

  return (
    <div className="flex flex-col gap-7">
      <SettingsSection title="New sessions">
        <SettingsRow
          label="Start in"
          description={
            kind === "linked"
              ? "Each session gets its own branch and directory, so edits are isolated."
              : "Edits land in the project's own checkout, beside anything open in your editor."
          }
          control={
            <div className="flex gap-1 rounded-lg border border-border/60 p-0.5">
              {KINDS.map(({ id, label }) => {
                const Icon = WORKTREE_GLYPH[id];
                return (
                  <button
                    key={id}
                    type="button"
                    onClick={() => setKind(id)}
                    aria-pressed={kind === id}
                    className={cn(
                      "flex cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[12px] transition-colors",
                      kind === id
                        ? "bg-secondary font-medium text-foreground-bright"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <Icon className="size-3.5" />
                    {label}
                  </button>
                );
              })}
            </div>
          }
        />
      </SettingsSection>
    </div>
  );
}
