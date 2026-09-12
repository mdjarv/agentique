import { createFileRoute } from "@tanstack/react-router";
import { BrainPage } from "~/components/brain/BrainPage";

/**
 * Memory — what the assistant remembers, under the assistant.
 *
 * `assistant_` rather than `assistant`: the thread's route renders a whole page
 * rather than an `<Outlet />`, so this is a sibling path and not a child of it
 * (the same spelling `discussions_.$channelId` uses).
 */
export const Route = createFileRoute("/assistant_/memory")({
  component: BrainPage,
});
