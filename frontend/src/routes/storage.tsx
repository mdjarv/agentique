import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { StoragePage } from "~/components/storage/StoragePage";

// `machine` is a paired machine's id; absent is this machine. An id that no
// longer names a machine falls back to this one rather than failing the page.
const searchSchema = z.object({
  machine: z.string().optional(),
});

export const Route = createFileRoute("/storage")({
  component: StorageRoute,
  validateSearch: searchSchema,
});

function StorageRoute() {
  const { machine } = Route.useSearch();
  return <StoragePage machine={machine} />;
}
