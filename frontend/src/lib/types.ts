import type { Project as WireProject } from "~/lib/generated-types";

/**
 * A project as held client-side. `machineId` tags projects that live on a
 * paired remote machine (multi-machine); absent = the primary machine.
 * Remote projects also get their `slug` suffixed at ingest ("agentique~ab12cd34")
 * so slug-addressed routes stay unambiguous when the same repo exists on
 * several machines. Wire payloads never carry either — tagging happens in
 * useMachineConnections.
 */
export type Project = WireProject & { machineId?: string };
