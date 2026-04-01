import { createFileRoute } from "@tanstack/react-router";
import { ChannelPanel } from "~/components/chat/ChannelPanel";
import { StatusPage } from "~/components/layout/PageHeader";
import { useAppStore } from "~/stores/app-store";
import { useTeamStore } from "~/stores/team-store";

export const Route = createFileRoute("/project/$projectSlug/channel/$channelId")({
  component: ChannelPage,
});

function ChannelPage() {
  const { projectSlug, channelId } = Route.useParams();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const team = useTeamStore((s) => s.teams[channelId]);

  if (!project) return null;

  if (!team) {
    return <StatusPage message="Channel not found" />;
  }

  return <ChannelPanel projectSlug={projectSlug} channelId={channelId} />;
}
