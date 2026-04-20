import { createFileRoute, Link } from "@tanstack/react-router";
import {
  AlertCircle,
  FolderGit2,
  FolderPlus,
  LayoutList,
  MousePointerClick,
  Play,
} from "lucide-react";
import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import { PageHeader } from "~/components/layout/PageHeader";
import { SessionStatusBadge } from "~/components/layout/session/SessionStatusBadge";
import { Button } from "~/components/ui/button";
import type { Project } from "~/lib/generated-types";
import { cn, relativeTime } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { type SessionData, useChatStore } from "~/stores/chat-store";

export const Route = createFileRoute("/")({
  component: HomePage,
});

const RESTING_STATES = new Set(["done", "stopped", "failed"]);

function HomePage() {
  const projects = useAppStore(useShallow((s) => s.projects));
  const projectsLoaded = useAppStore((s) => s.projectsLoaded);
  const sessions = useChatStore(useShallow((s) => s.sessions));
  const setSidebarOpen = useAppStore((s) => s.setSidebarOpen);

  const sessionList = useMemo(() => Object.values(sessions), [sessions]);

  const { active, running, pending, recent, perProject } = useMemo(() => {
    const activeSessions = sessionList.filter(
      (s) => !s.meta.completedAt && !RESTING_STATES.has(s.meta.state),
    );
    const running = activeSessions.filter((s) => s.meta.state === "running").length;
    const pending = activeSessions.filter((s) => !!(s.pendingApproval || s.pendingQuestion)).length;

    const recent = [...activeSessions]
      .sort((a, b) => {
        const score = (s: SessionData) => {
          if (s.pendingApproval || s.pendingQuestion) return 0;
          if (s.meta.state === "running") return 1;
          return 2;
        };
        const diff = score(a) - score(b);
        if (diff !== 0) return diff;
        return a.meta.name.localeCompare(b.meta.name);
      })
      .slice(0, 10);

    const perProject = new Map<string, number>();
    for (const s of activeSessions) {
      perProject.set(s.meta.projectId, (perProject.get(s.meta.projectId) ?? 0) + 1);
    }

    return { active: activeSessions.length, running, pending, recent, perProject };
  }, [sessionList]);

  const hasProjects = projects.length > 0;

  if (!projectsLoaded) {
    return <ShellLoading />;
  }

  if (!hasProjects) {
    return <EmptyShell onOpenSidebar={() => setSidebarOpen(true)} />;
  }

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <LayoutList className="size-4 text-muted-foreground" />
        <span className="font-semibold">Sessions</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-5xl px-6 py-8 space-y-10">
          {/* ── Intro ─────────────────────────────────── */}
          <header className="space-y-2">
            <h1 className="text-2xl font-semibold tracking-tight">Sessions</h1>
            <p className="text-sm text-muted-foreground max-w-prose leading-relaxed">
              One agent per session, each in its own worktree. Use a project to open or start a
              session, or jump to any currently active session below.
            </p>
          </header>

          {/* ── Stats ─────────────────────────────────── */}
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <StatCard label="Projects" value={projects.length} icon={FolderGit2} />
            <StatCard label="Active" value={active} icon={LayoutList} />
            <StatCard label="Running" value={running} icon={Play} accent={running > 0} />
            <StatCard
              label="Needs attention"
              value={pending}
              icon={AlertCircle}
              accent={pending > 0}
              accentColor="text-warning"
            />
          </div>

          {/* ── Projects ──────────────────────────────── */}
          <Section title="Projects">
            <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
              {projects.map((p) => (
                <ProjectCard key={p.id} project={p} activeCount={perProject.get(p.id) ?? 0} />
              ))}
            </div>
          </Section>

          {/* ── Recent active sessions ─────────────── */}
          {recent.length > 0 && (
            <Section title="Active sessions">
              <div className="rounded-lg border divide-y">
                {recent.map((s) => (
                  <RecentSessionRow
                    key={s.meta.id}
                    session={s}
                    projectSlug={projects.find((p) => p.id === s.meta.projectId)?.slug}
                  />
                ))}
              </div>
            </Section>
          )}
        </div>
      </div>
    </div>
  );
}

// ─── Sub-components ─────────────────────────────────────

function ShellLoading() {
  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold">Agentique</span>
      </PageHeader>
      <div className="flex-1 flex items-center justify-center text-sm text-muted-foreground">
        Loading…
      </div>
    </div>
  );
}

function EmptyShell({ onOpenSidebar }: { onOpenSidebar: () => void }) {
  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <span className="font-semibold">Agentique</span>
      </PageHeader>
      <div className="flex-1 flex flex-col items-center justify-center gap-4 px-4">
        <MousePointerClick className="h-10 w-10 text-muted-foreground/20" />
        <p className="text-muted-foreground text-sm">Create a project to get started.</p>
        <Button onClick={onOpenSidebar}>
          <FolderPlus className="h-4 w-4" />
          New project
        </Button>
      </div>
    </div>
  );
}

function StatCard({
  label,
  value,
  icon: Icon,
  accent,
  accentColor = "text-primary",
}: {
  label: string;
  value: number;
  icon: typeof FolderGit2;
  accent?: boolean;
  accentColor?: string;
}) {
  return (
    <div className="rounded-lg border bg-card/40 px-4 py-3">
      <div className="flex items-center gap-2 text-xs uppercase tracking-wider text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div
        className={cn(
          "mt-1 text-2xl font-semibold tabular-nums",
          accent && value > 0 && accentColor,
        )}
      >
        {value}
      </div>
    </div>
  );
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
        {title}
      </h2>
      {children}
    </section>
  );
}

function ProjectCard({ project, activeCount }: { project: Project; activeCount: number }) {
  return (
    <Link
      to="/project/$projectSlug"
      params={{ projectSlug: project.slug }}
      className="group block rounded-lg border bg-card/40 px-4 py-3 transition-colors hover:bg-card/80 hover:border-primary/30"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex items-center gap-2 min-w-0">
          {project.icon && <span className="text-lg shrink-0">{project.icon}</span>}
          <div className="min-w-0">
            <div className="font-medium text-sm truncate">{project.name}</div>
            <div className="text-[11px] text-muted-foreground truncate">{project.slug}</div>
          </div>
        </div>
        {activeCount > 0 && (
          <div className="shrink-0 rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary tabular-nums">
            {activeCount} active
          </div>
        )}
      </div>
    </Link>
  );
}

function RecentSessionRow({
  session,
  projectSlug,
}: {
  session: SessionData;
  projectSlug: string | undefined;
}) {
  const { meta } = session;
  const hasPending = !!(session.pendingApproval || session.pendingQuestion);

  const content = (
    <div className="flex items-center gap-3 px-4 py-2.5 text-sm hover:bg-muted/40 transition-colors">
      <SessionStatusBadge
        state={meta.state}
        connected={meta.connected}
        hasUnseenCompletion={false}
        hasPendingApproval={hasPending}
        isPlanning={session.planMode}
        size="sm"
      />
      <div className="min-w-0 flex-1">
        <div className="truncate font-medium">{meta.name || "Untitled"}</div>
        {meta.worktreeBranch && (
          <div className="truncate text-[11px] text-muted-foreground">{meta.worktreeBranch}</div>
        )}
      </div>
      {hasPending && (
        <span className="rounded-full bg-warning/15 px-2 py-0.5 text-[10px] font-medium text-warning">
          Needs input
        </span>
      )}
      {meta.updatedAt && (
        <span className="text-[11px] text-muted-foreground tabular-nums shrink-0">
          {relativeTime(meta.updatedAt)}
        </span>
      )}
    </div>
  );

  if (!projectSlug) return <div>{content}</div>;
  return (
    <Link
      to="/project/$projectSlug/session/$sessionShortId"
      params={{
        projectSlug,
        sessionShortId: meta.id.split("-")[0] ?? meta.id,
      }}
      className="block"
    >
      {content}
    </Link>
  );
}
