import { createFileRoute } from "@tanstack/react-router";
import { ChannelPanel } from "~/components/chat/ChannelPanel";
import { StatusPage } from "~/components/layout/PageHeader";
import { useAppStore } from "~/stores/app-store";
import { useChannelStore } from "~/stores/channel-store";

export const Route = createFileRoute("/project/$projectSlug/channel/$channelId")({
  component: ChannelPage,
});

function ChannelPage() {
  const { projectSlug, channelId } = Route.useParams();
  const project = useAppStore((s) => s.projects.find((p) => p.slug === projectSlug));
  const channel = useChannelStore((s) => s.channels[channelId]);

  if (!project) return <StatusPage message="Project not found" />;

  if (!channel) {
    return <StatusPage message="Channel not found" />;
  }

  return <ChannelPanel projectSlug={projectSlug} channelId={channelId} />;
}
