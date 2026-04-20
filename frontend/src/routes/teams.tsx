import { createFileRoute, Link } from "@tanstack/react-router";
import { Hash, MessageSquare, Plus, UserPlus, Users } from "lucide-react";
import { useMemo } from "react";
import { useShallow } from "zustand/react/shallow";
import { PageHeader } from "~/components/layout/PageHeader";
import { TeamCard } from "~/components/team/TeamCard";
import { TeamFormDialog } from "~/components/team/TeamFormDialog";
import { Button } from "~/components/ui/button";
import type { AgentProfileInfo } from "~/lib/team-actions";
import { cn } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";
import { useTeamStore } from "~/stores/team-store";

export const Route = createFileRoute("/teams")({
  component: TeamsDashboard,
});

function TeamsDashboard() {
  const channels = useChannelStore(useShallow((s) => s.channels));
  const projects = useAppStore(useShallow((s) => s.projects));
  const profiles = useTeamStore(useShallow((s) => s.profiles));
  const teams = useTeamStore(useShallow((s) => s.teams));
  const loaded = useTeamStore((s) => s.loaded);

  const channelList = useMemo(() => Object.values(channels), [channels]);
  const profileList = useMemo(() => Object.values(profiles), [profiles]);
  const teamList = useMemo(() => Object.values(teams), [teams]);

  const activeChannels = channelList.length;
  const totalTeams = teamList.length;
  const totalProfiles = profileList.length;

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <Users className="size-4 text-muted-foreground" />
        <span className="font-semibold">Teams</span>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-5xl px-6 py-8 space-y-10">
          {/* ── Intro ─────────────────────────────────── */}
          <header className="space-y-2">
            <h1 className="text-2xl font-semibold tracking-tight">Teams</h1>
            <p className="text-sm text-muted-foreground max-w-prose leading-relaxed">
              Persistent agent identities, grouped into teams, that coordinate across channels. Set
              up profiles with their own system prompt, presets, and default model, then assemble
              them into teams for recurring workflows.
            </p>
          </header>

          {/* ── Stats ─────────────────────────────────── */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <StatCard label="Active channels" value={activeChannels} icon={Hash} />
            <StatCard label="Teams" value={totalTeams} icon={Users} />
            <StatCard label="Agent profiles" value={totalProfiles} icon={MessageSquare} />
          </div>

          {/* ── Teams ─────────────────────────────────── */}
          <Section
            title="Teams"
            action={
              <TeamFormDialog
                trigger={
                  <Button size="sm" variant="outline">
                    <Plus className="size-3.5" />
                    New team
                  </Button>
                }
              />
            }
          >
            {!loaded ? (
              <EmptyState>Loading…</EmptyState>
            ) : teamList.length === 0 ? (
              <EmptyState>
                No teams yet. Create a team to group profiles for a recurring workflow.
              </EmptyState>
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                {teamList.map((team) => (
                  <TeamCard key={team.id} team={team} allProfiles={profileList} />
                ))}
              </div>
            )}
          </Section>

          {/* ── Profiles ──────────────────────────────── */}
          <Section
            title="Agent profiles"
            action={
              <Button size="sm" variant="outline" asChild>
                <Link to="/teams/personas/new">
                  <UserPlus className="size-3.5" />
                  New profile
                </Link>
              </Button>
            }
          >
            {!loaded ? (
              <EmptyState>Loading…</EmptyState>
            ) : profileList.length === 0 ? (
              <EmptyState>
                No agent profiles yet. Create one to define a reusable identity and behavior
                defaults for sessions.
              </EmptyState>
            ) : (
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
                {profileList.map((profile) => (
                  <ProfileCard
                    key={profile.id}
                    profile={profile}
                    projectName={
                      profile.projectId
                        ? (projects.find((p) => p.id === profile.projectId)?.name ?? null)
                        : null
                    }
                  />
                ))}
              </div>
            )}
          </Section>
        </div>
      </div>
    </div>
  );
}

// ─── Sub-components ─────────────────────────────────────

function StatCard({
  label,
  value,
  icon: Icon,
}: {
  label: string;
  value: number;
  icon: typeof Hash;
}) {
  return (
    <div className="rounded-lg border bg-card/40 px-4 py-3">
      <div className="flex items-center gap-2 text-xs uppercase tracking-wider text-muted-foreground">
        <Icon className="size-3.5" />
        {label}
      </div>
      <div className="mt-1 text-2xl font-semibold tabular-nums">{value}</div>
    </div>
  );
}

function Section({
  title,
  action,
  children,
}: {
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-semibold uppercase tracking-wider text-muted-foreground">
          {title}
        </h2>
        {action}
      </div>
      {children}
    </section>
  );
}

function EmptyState({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-lg border border-dashed bg-card/20 px-6 py-8 text-center text-sm text-muted-foreground">
      {children}
    </div>
  );
}

function ProfileCard({
  profile,
  projectName,
}: {
  profile: AgentProfileInfo;
  projectName: string | null;
}) {
  return (
    <Link
      to="/teams/personas/$profileId"
      params={{ profileId: profile.id }}
      className={cn(
        "group block rounded-lg border bg-card/40 px-4 py-3 transition-colors hover:bg-card/80 hover:border-primary/30",
      )}
    >
      <div className="flex items-start gap-3">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md border bg-background text-lg">
          {profile.avatar || "🤖"}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-1.5">
            <span className="truncate font-medium text-sm">{profile.name || "Unnamed"}</span>
          </div>
          {profile.role && (
            <div className="truncate text-xs text-muted-foreground mt-0.5">{profile.role}</div>
          )}
          {projectName && (
            <div className="mt-2 inline-flex items-center gap-1 rounded-full border bg-background/60 px-2 py-0.5 text-[10px] text-muted-foreground">
              <Hash className="size-2.5" />
              {projectName}
            </div>
          )}
        </div>
      </div>
    </Link>
  );
}
