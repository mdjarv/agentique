import { useMemo } from "react";
import { ScrollArea } from "~/components/ui/scroll-area";
import { Separator } from "~/components/ui/separator";
import { useTeamStore } from "~/stores/team-store";
import { InteractionRow } from "./InteractionRow";
import { ProfileEditorDialog } from "./ProfileEditorDialog";
import { ProfileRow } from "./ProfileRow";
import { TeamCard } from "./TeamCard";
import { CreateTeamTrigger, TeamFormDialog } from "./TeamFormDialog";

export function TeamPanel() {
  const teams = useTeamStore((s) => s.teams);
  const profiles = useTeamStore((s) => s.profiles);
  const interactions = useTeamStore((s) => s.interactions);
  const loaded = useTeamStore((s) => s.loaded);

  const teamList = useMemo(() => Object.values(teams), [teams]);
  const profileList = useMemo(() => Object.values(profiles), [profiles]);

  const allInteractions = useMemo(() => {
    const all = Object.values(interactions).flat();
    all.sort((a, b) => (b.createdAt > a.createdAt ? 1 : -1));
    return all.slice(0, 20);
  }, [interactions]);

  if (!loaded) {
    return (
      <div className="flex-1 flex items-center justify-center text-muted-foreground text-sm">
        Loading...
      </div>
    );
  }

  return (
    <ScrollArea className="flex-1">
      <div className="p-3 space-y-4">
        {/* Teams */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Teams
            </h3>
            <TeamFormDialog trigger={<CreateTeamTrigger />} />
          </div>
          {teamList.length === 0 ? (
            <p className="text-xs text-muted-foreground/60 px-1">No teams yet</p>
          ) : (
            teamList.map((team) => <TeamCard key={team.id} team={team} allProfiles={profileList} />)
          )}
        </div>

        <Separator />

        {/* Agent Profiles */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Agent Profiles
            </h3>
            <ProfileEditorDialog />
          </div>
          {profileList.length === 0 ? (
            <p className="text-xs text-muted-foreground/60 px-1">No profiles yet</p>
          ) : (
            profileList.map((profile) => <ProfileRow key={profile.id} profile={profile} />)
          )}
        </div>

        {/* Persona interactions */}
        {allInteractions.length > 0 && (
          <>
            <Separator />
            <div className="space-y-2">
              <h3 className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Persona Activity
              </h3>
              <div className="space-y-1.5">
                {allInteractions.map((ix) => (
                  <InteractionRow key={ix.id} interaction={ix} profiles={profiles} />
                ))}
              </div>
            </div>
          </>
        )}
      </div>
    </ScrollArea>
  );
}
