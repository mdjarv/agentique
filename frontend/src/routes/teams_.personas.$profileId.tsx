import { createFileRoute, useNavigate } from "@tanstack/react-router";
import { ArrowLeft, Trash2 } from "lucide-react";
import { useCallback } from "react";
import { toast } from "sonner";
import { PageHeader, StatusPage } from "~/components/layout/PageHeader";
import { ProfileForm } from "~/components/team/ProfileForm";
import { Button } from "~/components/ui/button";
import { useWebSocket } from "~/hooks/useWebSocket";
import { deleteAgentProfile } from "~/lib/team-actions";
import { getErrorMessage } from "~/lib/utils";
import { useTeamStore } from "~/stores/team-store";

export const Route = createFileRoute("/teams_/personas/$profileId")({
  component: EditProfilePage,
});

function EditProfilePage() {
  const { profileId } = Route.useParams();
  const navigate = useNavigate();
  const ws = useWebSocket();
  const profile = useTeamStore((s) => s.profiles[profileId]);

  const handleDelete = useCallback(async () => {
    if (!profile) return;
    if (!confirm(`Delete agent profile "${profile.name}"? This cannot be undone.`)) return;
    try {
      await deleteAgentProfile(ws, profile.id);
      useTeamStore.getState().removeProfile(profile.id);
      toast.success("Profile deleted");
      navigate({ to: "/teams" });
    } catch (e) {
      toast.error(getErrorMessage(e, "Failed to delete profile"));
    }
  }, [ws, profile, navigate]);

  if (!profile) {
    return (
      <StatusPage
        header={<span className="font-semibold">Profile not found</span>}
        message="This agent profile does not exist or has been deleted."
      />
    );
  }

  return (
    <div className="flex flex-col h-full">
      <PageHeader>
        <Button
          variant="ghost"
          size="icon"
          className="-ml-1 size-7"
          onClick={() => navigate({ to: "/teams" })}
          aria-label="Back to Teams"
        >
          <ArrowLeft className="size-4" />
        </Button>
        <span className="flex items-center gap-2 font-semibold">
          {profile.avatar && <span className="text-base">{profile.avatar}</span>}
          {profile.name || "Unnamed"}
        </span>
        <div className="ml-auto">
          <Button
            variant="ghost"
            size="sm"
            onClick={handleDelete}
            className="text-muted-foreground hover:text-destructive"
          >
            <Trash2 className="size-3.5" />
            Delete
          </Button>
        </div>
      </PageHeader>
      <div className="flex-1 overflow-y-auto">
        <ProfileForm
          profile={profile}
          onSaved={() => {
            toast.success("Profile saved");
          }}
          onCancel={() => navigate({ to: "/teams" })}
        />
      </div>
    </div>
  );
}
