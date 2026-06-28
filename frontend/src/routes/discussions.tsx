import { createFileRoute, Link, useNavigate } from "@tanstack/react-router";
import { Loader2, MessagesSquare, Minus, Plus, Sparkles } from "lucide-react";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { useShallow } from "zustand/react/shallow";
import { PageHeader } from "~/components/layout/PageHeader";
import { Button } from "~/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "~/components/ui/dropdown-menu";
import { Input } from "~/components/ui/input";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import {
  type DiscussionMode,
  type DiscussionScope,
  startDiscussion,
} from "~/lib/discussion-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useDiscussionStore } from "~/stores/discussion-store";
import { useTeamStore } from "~/stores/team-store";

export const Route = createFileRoute("/discussions")({
  component: DiscussionsPage,
});

interface Selected {
  agentProfileId: string;
  name: string;
  writeAccess: boolean;
}

function DiscussionsPage() {
  const ws = useWebSocket();
  const navigate = useNavigate();
  const projects = useAppStore(useShallow((s) => s.projects));
  const profiles = useTeamStore(useShallow((s) => s.profiles));
  const discussions = useDiscussionStore(useShallow((s) => s.discussions));

  const profileList = useMemo(() => Object.values(profiles), [profiles]);
  const activeList = useMemo(() => Object.values(discussions), [discussions]);

  const [groupName, setGroupName] = useState("");
  const [projectId, setProjectId] = useState(projects[0]?.id ?? "");
  const [mode, setMode] = useState<DiscussionMode>("round-robin");
  const [scope, setScope] = useState<DiscussionScope>("web-only");
  const [autoCommit, setAutoCommit] = useState(true);
  const [selected, setSelected] = useState<Selected[]>([]);
  const [prompt, setPrompt] = useState("");
  const [sending, setSending] = useState(false);

  const projectName = projects.find((p) => p.id === projectId)?.name ?? "Select project";

  // Personas available for this project: global (no projectId) + this project's.
  const available = useMemo(
    () =>
      profileList.filter(
        (p) =>
          (!p.projectId || p.projectId === projectId) &&
          !selected.some((s) => s.agentProfileId === p.id),
      ),
    [profileList, projectId, selected],
  );

  const canSubmit = !sending && projectId !== "" && selected.length >= 2 && prompt.trim() !== "";

  async function handleStart() {
    if (!canSubmit) return;
    setSending(true);
    try {
      const info = await startDiscussion(ws, {
        projectId,
        groupName: groupName.trim() || "Discussion",
        mode,
        scope,
        autoCommit,
        personas: selected.map((s) => ({
          agentProfileId: s.agentProfileId,
          name: s.name,
          model: "",
          effort: "",
          writeAccess: s.writeAccess,
          noNamePrefix: false,
        })),
        prompt: prompt.trim(),
      });
      useDiscussionStore.getState().setDiscussion(info);
      navigate({ to: "/discussions/$channelId", params: { channelId: info.channelId } });
    } catch (err) {
      toast.error(getErrorMessage(err, "Failed to start discussion"));
      setSending(false);
    }
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader>
        <MessagesSquare className="size-4 text-muted-foreground" />
        <span className="font-semibold">Discussion Groups</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl space-y-8 px-6 py-8">
          <header className="space-y-2">
            <h1 className="text-2xl font-semibold tracking-tight">Discussion Groups</h1>
            <p className="max-w-prose text-sm leading-relaxed text-muted-foreground">
              Pick a panel of personas, drop in a prompt, and watch them discuss it with each other
              across rounds you drive. Each persona is a real session — they can read the repo,
              search the web, and (if you grant write access) edit code.
            </p>
          </header>

          {/* Active discussions */}
          {activeList.length > 0 && (
            <section className="space-y-2">
              <h2 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground/60">
                Active
              </h2>
              <div className="grid gap-2 sm:grid-cols-2">
                {activeList.map((d) => (
                  <Link
                    key={d.channelId}
                    to="/discussions/$channelId"
                    params={{ channelId: d.channelId }}
                    className="rounded-xl border border-border/60 bg-card p-3 transition-colors hover:border-border"
                  >
                    <div className="flex items-center gap-2">
                      <span className="truncate font-semibold text-foreground">{d.groupName}</span>
                      {d.running && <Loader2 className="size-3.5 animate-spin text-teal" />}
                    </div>
                    <div className="mt-1 truncate text-xs text-muted-foreground">
                      {d.personas.join(", ")} · round {d.round}
                    </div>
                  </Link>
                ))}
              </div>
            </section>
          )}

          {/* New discussion */}
          <section className="space-y-4 rounded-2xl border border-border bg-card/40 p-5">
            <h2 className="flex items-center gap-2 text-sm font-semibold text-foreground">
              <Sparkles className="size-4 text-teal" /> New discussion
            </h2>

            <div className="flex flex-wrap items-center gap-2">
              <Input
                value={groupName}
                onChange={(e) => setGroupName(e.target.value)}
                placeholder="Group name"
                className="h-8 flex-1 text-sm"
              />
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" size="sm">
                    {projectName}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  {projects.map((p) => (
                    <DropdownMenuItem key={p.id} onClick={() => setProjectId(p.id)}>
                      {p.name}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            </div>

            {/* Personas */}
            <div className="space-y-2">
              <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground/60">
                Personas {selected.length > 0 && `· ${selected.length}`}
              </div>
              {selected.map((s) => (
                <div
                  key={s.agentProfileId}
                  className="flex items-center gap-2 rounded-lg border border-border/60 bg-popover px-3 py-2"
                >
                  <span className="flex-1 text-sm font-medium text-foreground">{s.name}</span>
                  <button
                    type="button"
                    onClick={() =>
                      setSelected((prev) =>
                        prev.map((p) =>
                          p.agentProfileId === s.agentProfileId
                            ? { ...p, writeAccess: !p.writeAccess }
                            : p,
                        ),
                      )
                    }
                    className={cn(
                      "rounded-md border px-2 py-1 text-xs font-semibold transition-colors",
                      s.writeAccess
                        ? "border-teal/40 bg-teal/10 text-teal"
                        : "border-border bg-muted/40 text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {s.writeAccess ? "✎ write" : "read-only"}
                  </button>
                  <Button
                    variant="ghost"
                    size="xs"
                    onClick={() =>
                      setSelected((prev) =>
                        prev.filter((p) => p.agentProfileId !== s.agentProfileId),
                      )
                    }
                  >
                    <Minus className="size-3" />
                  </Button>
                </div>
              ))}
              {available.length > 0 && (
                <div className="flex flex-wrap gap-1.5">
                  {available.map((p) => (
                    <button
                      key={p.id}
                      type="button"
                      onClick={() =>
                        setSelected((prev) => [
                          ...prev,
                          { agentProfileId: p.id, name: p.name, writeAccess: false },
                        ])
                      }
                      className="flex items-center gap-1.5 rounded-full border border-border bg-muted/30 px-3 py-1 text-xs text-muted-foreground transition-colors hover:border-teal/40 hover:text-foreground"
                    >
                      <Plus className="size-3" />
                      {p.avatar} {p.name}
                    </button>
                  ))}
                </div>
              )}
              {selected.length < 2 && (
                <p className="text-xs text-muted-foreground/70">Add at least 2 personas.</p>
              )}
            </div>

            {/* Mode + scope + auto-commit */}
            <div className="flex flex-wrap items-center gap-2">
              <Toggle
                left="Sequential"
                right="Parallel"
                value={mode === "parallel"}
                onChange={(r) => setMode(r ? "parallel" : "round-robin")}
              />
              <Toggle
                left="◧ Repo-backed"
                right="🌐 Web-only"
                value={scope === "web-only"}
                onChange={(r) => setScope(r ? "web-only" : "repo-backed")}
              />
              {scope === "repo-backed" && (
                <button
                  type="button"
                  onClick={() => setAutoCommit((v) => !v)}
                  className={cn(
                    "flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors",
                    autoCommit
                      ? "border-teal/40 bg-teal/10 text-teal"
                      : "border-border text-muted-foreground hover:text-foreground",
                  )}
                >
                  <span className="grid size-4 place-items-center rounded-sm border border-current text-[10px]">
                    {autoCommit ? "✓" : ""}
                  </span>
                  Auto-commit writer turns
                </button>
              )}
            </div>

            <Textarea
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="What should the group discuss?"
              className="min-h-[90px] resize-none text-sm"
            />

            <div className="flex items-center justify-between">
              <span className="text-xs text-muted-foreground">
                {selected.length} personas · {mode} · {scope}
              </span>
              <Button disabled={!canSubmit} onClick={() => void handleStart()}>
                {sending ? (
                  <Loader2 className="size-3.5 animate-spin" />
                ) : (
                  <MessagesSquare className="size-3.5" />
                )}
                Start discussion
              </Button>
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}

function Toggle({
  left,
  right,
  value,
  onChange,
}: {
  left: string;
  right: string;
  value: boolean;
  onChange: (right: boolean) => void;
}) {
  return (
    <div className="flex rounded-lg border border-border bg-muted/30 p-0.5">
      <button
        type="button"
        onClick={() => onChange(false)}
        className={cn(
          "rounded-md px-3 py-1 text-xs font-medium transition-colors",
          !value ? "bg-popover text-foreground" : "text-muted-foreground hover:text-foreground",
        )}
      >
        {left}
      </button>
      <button
        type="button"
        onClick={() => onChange(true)}
        className={cn(
          "rounded-md px-3 py-1 text-xs font-medium transition-colors",
          value ? "bg-popover text-foreground" : "text-muted-foreground hover:text-foreground",
        )}
      >
        {right}
      </button>
    </div>
  );
}
