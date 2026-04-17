import { Trash2, Users } from "lucide-react";
import { useCallback, useMemo } from "react";
import { toast } from "sonner";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { AgentProfileInfo, TeamInfo } from "~/lib/team-actions";
import { addTeamMember, deleteTeam, removeTeamMember } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";
import { useTeamStore } from "~/stores/team-store";
import { TeamFormDialog } from "./TeamFormDialog";
import { TeamMemberRow } from "./TeamMemberRow";

export function TeamCard({
  team,
  allProfiles,
}: {
  team: TeamInfo;
  allProfiles: AgentProfileInfo[];
}) {
  const ws = useWebSocket();
  const memberIds = useMemo(() => new Set(team.members.map((m) => m.id)), [team.members]);
  const unassigned = useMemo(
    () => allProfiles.filter((p) => !memberIds.has(p.id)),
    [allProfiles, memberIds],
  );

  const handleAddMember = useCallback(
    async (profileId: string) => {
      try {
        const updated = await addTeamMember(ws, {
          teamId: team.id,
          agentProfileId: profileId,
          sortOrder: team.members.length,
        });
        useTeamStore.getState().updateTeam(updated);
      } catch (e) {
        toast.error(getErrorMessage(e, "Operation failed"));
      }
    },
    [ws, team.id, team.members.length],
  );

  const handleRemoveMember = useCallback(
    async (profileId: string) => {
      try {
        const updated = await removeTeamMember(ws, { teamId: team.id, agentProfileId: profileId });
        useTeamStore.getState().updateTeam(updated);
      } catch (e) {
        toast.error(getErrorMessage(e, "Operation failed"));
      }
    },
    [ws, team.id],
  );

  const handleDelete = useCallback(async () => {
    try {
      await deleteTeam(ws, team.id);
      useTeamStore.getState().removeTeam(team.id);
      toast.success("Team deleted");
    } catch (e) {
      toast.error(getErrorMessage(e, "Operation failed"));
    }
  }, [ws, team.id]);

  return (
    <div className="rounded-lg border bg-card/50 p-2.5 space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Users className="size-3.5 text-muted-foreground" />
          <TeamFormDialog
            team={team}
            trigger={
              <button type="button" className="font-semibold text-sm hover:underline">
                {team.name}
              </button>
            }
          />
        </div>
        <Button variant="ghost" size="icon" className="size-6" onClick={handleDelete}>
          <Trash2 className="size-3" />
        </Button>
      </div>

      {team.description && (
        <p className="text-xs text-muted-foreground pl-5.5">{team.description}</p>
      )}

      <div className="space-y-1 pl-5.5">
        {team.members.map((m) => (
          <TeamMemberRow key={m.id} member={m} teamId={team.id} onRemove={handleRemoveMember} />
        ))}

        {unassigned.length > 0 && (
          <div className="pt-1">
            <select
              className="text-xs bg-transparent border rounded px-1 py-0.5 text-muted-foreground w-full"
              value=""
              onChange={(e) => {
                if (e.target.value) handleAddMember(e.target.value);
              }}
            >
              <option value="">+ Add member...</option>
              {unassigned.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name} {p.role && `(${p.role})`}
                </option>
              ))}
            </select>
          </div>
        )}
      </div>
    </div>
  );
}
