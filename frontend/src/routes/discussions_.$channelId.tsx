import { createFileRoute } from "@tanstack/react-router";
import { DiscussionPanel } from "~/components/discussion/DiscussionPanel";

export const Route = createFileRoute("/discussions_/$channelId")({
  component: DiscussionPanelPage,
});

function DiscussionPanelPage() {
  const { channelId } = Route.useParams();
  return <DiscussionPanel channelId={channelId} />;
}
