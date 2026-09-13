import { createFileRoute } from "@tanstack/react-router";
import { AssistantPoliciesPage } from "~/components/assistant/AssistantPolicies";

/**
 * Policies — the standing instructions, under the assistant.
 *
 * `assistant_` rather than `assistant`, the same spelling memory uses: the
 * thread's route renders a whole page rather than an `<Outlet />`, so this is a
 * sibling path and not a child of it.
 */
export const Route = createFileRoute("/assistant_/policies")({
  component: AssistantPoliciesPage,
});
