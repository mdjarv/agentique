import { create } from "zustand";
import type { UpdateProgress, UpdateStatus } from "~/lib/generated-types";
import {
  applyUpdate,
  awaitRestart,
  cancelUpdate,
  fetchUpdateStatus,
  PRIMARY_MACHINE_KEY,
  type UpdateKind,
} from "~/lib/update-api";
import { sourceWantsAttention } from "~/lib/update-source";
import { useMachineStore } from "~/stores/machine-store";

/**
 * What each machine says about its own version, and how an upgrade in flight
 * on it is going (docs/upgrades.md).
 *
 * Keyed by machine: PRIMARY_MACHINE_KEY for the server serving this SPA, the
 * machineId for each paired remote. Nothing here is persisted, and nothing here
 * can be waved away: the footer's mark is a dot (decision U2), which is quiet
 * enough not to need silencing — and an update that can be silenced is one
 * nobody applies.
 *
 * The two client-only phases exist because the server cannot narrate its own
 * replacement: after `restarting` nobody is left to report, so the client
 * polls the unauthenticated descriptor and reports the version it ACTUALLY
 * found.
 */
export type ClientPhase = "reconnecting" | "confirmed" | "unconfirmed";

export interface Flight {
  /** Server-reported progress, from the WS topic or a status read. */
  progress?: UpdateProgress;
  /** Set once the server has gone quiet and we are waiting for it to return. */
  clientPhase?: ClientPhase;
  /** The version the machine came back on — what it IS, not what we asked for. */
  foundVersion?: string;
}

interface UpdateState {
  statuses: Record<string, UpdateStatus>;
  /** Machines whose check is in flight, so a button can say so. */
  checking: Record<string, boolean>;
  /** Machines whose last fetch failed (unreachable, or too old to answer). */
  errors: Record<string, string>;
  /** Live upgrades, keyed like statuses. */
  flights: Record<string, Flight>;

  /** Fetch one machine's status. `refresh` forces that server to re-check. */
  fetch: (key: string, refresh?: boolean) => Promise<void>;
  /** Fetch several machines concurrently — see `machineKeys()` for the set. */
  fetchAll: (keys: string[], refresh?: boolean) => Promise<void>;
  /** Fold a progress push (or a status read) into the flight for a machine. */
  applyProgress: (key: string, progress: UpdateProgress) => void;
  /** Start an upgrade on one machine — now, when idle, or over the top of the
   *  turns in flight. `kind` picks the channel; absent means release. */
  apply: (
    key: string,
    opts?: { force?: boolean; whenIdle?: boolean; kind?: UpdateKind },
  ) => Promise<void>;
  /** Disarm an armed upgrade, or cancel one that has not installed anything. */
  cancel: (key: string) => Promise<void>;
  /** Forget a finished flight so the row goes back to normal. */
  clearFlight: (key: string) => void;
  /** Poll a restarting machine's descriptor until it answers on a new version.
   *  Driven by applyProgress; not called directly. */
  watchRestart: (key: string, was: string) => Promise<void>;
}

/** Where to reach a machine when its socket is gone — the restart watch polls
 *  this directly rather than going through the (also-restarting) API. */
function baseUrlFor(key: string): string {
  if (key === PRIMARY_MACHINE_KEY) return window.location.origin;
  return useMachineStore.getState().machines[key]?.baseUrl ?? window.location.origin;
}

export const useUpdateStore = create<UpdateState>((set, get) => ({
  statuses: {},
  checking: {},
  errors: {},
  flights: {},

  fetch: async (key, refresh = false) => {
    set((s) => ({ checking: { ...s.checking, [key]: true } }));
    try {
      const status = await fetchUpdateStatus(key, refresh);
      set((s) => {
        const errors = { ...s.errors };
        delete errors[key];
        return { statuses: { ...s.statuses, [key]: status }, errors };
      });
      // Progress is state as well as events: a client that reloaded
      // mid-upgrade learns about it from this read, not from a push it missed.
      if (status.progress) get().applyProgress(key, status.progress);
    } catch (err) {
      // A machine that cannot answer is not a failure to shout about: keep
      // whatever it last said and record why the refresh didn't land.
      set((s) => ({
        errors: { ...s.errors, [key]: err instanceof Error ? err.message : String(err) },
      }));
    } finally {
      set((s) => {
        const checking = { ...s.checking };
        delete checking[key];
        return { checking };
      });
    }
  },

  fetchAll: async (keys, refresh = false) => {
    // One request per machine, in parallel: every server answers for itself,
    // and a machine that is asleep must not hold up the ones that are awake.
    await Promise.all(keys.map((key) => get().fetch(key, refresh)));
  },

  applyProgress: (key, progress) => {
    const previous = get().flights[key];
    // Ignore a push that is older than what we already have — WS delivery and
    // a status read race, and the point of holding both is that whichever
    // arrives first wins, not whichever arrives last.
    if (previous?.progress && previous.progress.updatedAt > progress.updatedAt) return;

    set((s) => ({ flights: { ...s.flights, [key]: { ...s.flights[key], progress } } }));

    // `restarting` is the last thing this server will ever say: it is being
    // replaced by the process it is announcing. Take over from here.
    if (progress.phase === "restarting" && previous?.clientPhase === undefined) {
      void get().watchRestart(key, progress.from);
    }
  },

  apply: async (key, opts = {}) => {
    const status = get().statuses[key];
    if (!status) throw new Error("no version information for that machine");
    // `expect` is the staleness guard, and each channel measures it in its own
    // units: a tag for a release, the branch head's commit for a source build.
    // A restart installs nothing, so there is nothing to be stale about.
    const expect =
      opts.kind === "source"
        ? (status.source?.head ?? "")
        : opts.kind === "restart"
          ? ""
          : status.latest;
    await applyUpdate(key, expect, opts);
    // The 202's body is the queued progress (or the arming); the WS topic
    // carries the rest. Read it back so a client whose socket is slow still
    // starts narrating, and so an arming lands in the status immediately.
    await get().fetch(key);
  },

  cancel: async (key) => {
    await cancelUpdate(key);
    void get().fetch(key);
  },

  clearFlight: (key) =>
    set((s) => {
      if (!s.flights[key]) return s;
      const flights = { ...s.flights };
      delete flights[key];
      return { flights };
    }),

  watchRestart: async (key, was) => {
    set((s) => ({
      flights: { ...s.flights, [key]: { ...s.flights[key], clientPhase: "reconnecting" } },
    }));

    // The descriptor is the fast path: unauthenticated, so it answers the
    // moment the process is listening again.
    let { version, changed } = await awaitRestart(baseUrlFor(key), was);

    // Then confirm against the machine's own authenticated answer. The
    // descriptor can be unreachable for reasons that have nothing to do with
    // the upgrade (a dev proxy, a reverse proxy that only forwards /api), and
    // reporting "still on the old version" when the machine is plainly running
    // the new one is the one lie this flow must not tell.
    await get().fetch(key);
    const current = get().statuses[key]?.current;
    if (current) {
      version = current;
      changed = current !== was;
    }

    set((s) => ({
      flights: {
        ...s.flights,
        [key]: {
          ...s.flights[key],
          clientPhase: changed ? "confirmed" : "unconfirmed",
          foundVersion: version,
        },
      },
    }));
  },
}));

/**
 * Machine keys with something waiting. Ordered primary-first.
 *
 * Two independent claims count: a published release this machine is behind, and
 * a local checkout that has moved past the build it is running. They have
 * different costs and neither hides the other (docs/upgrades.md), so a machine
 * appears here if either holds.
 */
export function behindKeys(statuses: Record<string, UpdateStatus>): string[] {
  return Object.keys(statuses)
    .filter((key) => {
      const status = statuses[key];
      return Boolean(status?.behind) || sourceWantsAttention(status?.source);
    })
    .sort((a, b) => (a === PRIMARY_MACHINE_KEY ? -1 : b === PRIMARY_MACHINE_KEY ? 1 : 0));
}

/** Machine keys behind a published RELEASE, which is what the chip names when
 *  it can name a version. A source verdict has no version to name. */
export function releaseBehindKeys(statuses: Record<string, UpdateStatus>): string[] {
  return behindKeys(statuses).filter((key) => statuses[key]?.behind);
}

/** Whether a flight is still going, so the UI keeps narrating. */
export function flightActive(flight: Flight | undefined): boolean {
  if (!flight) return false;
  if (flight.clientPhase) return flight.clientPhase === "reconnecting";
  const phase = flight.progress?.phase;
  return phase !== undefined && phase !== "failed" && phase !== "cancelled";
}
