import { createFileRoute } from "@tanstack/react-router";
import { ScheduleListPage } from "~/components/schedules/ScheduleListPage";

export const Route = createFileRoute("/schedules")({
  component: ScheduleListPage,
});
