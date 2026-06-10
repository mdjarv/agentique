import { createFileRoute } from "@tanstack/react-router";
import { StoragePage } from "~/components/storage/StoragePage";

export const Route = createFileRoute("/storage")({
  component: StoragePage,
});
