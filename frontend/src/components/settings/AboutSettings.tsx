/** Settings › About — what this build is, what it has switched on, and what
 *  every machine you work across is running (docs/upgrades.md). */
import { Link } from "@tanstack/react-router";
import { RefreshCw } from "lucide-react";
import { SettingsRow, SettingsSection } from "~/components/settings/SettingsLayout";
import { Button } from "~/components/ui/button";
import type { UpdateCLIStatus, UpdateStatus } from "~/lib/generated-types";
import { checkedAgo, PRIMARY_MACHINE_KEY } from "~/lib/update-api";
import { cn, relativeTime } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";
import { useMachineStore } from "~/stores/machine-store";
import { useUpdateStore } from "~/stores/update-store";

/** The one line under a CLI's name. Warnings come first when present: a second
 *  copy on PATH means the version beside it has stopped describing the binary
 *  that actually runs, which outranks how it was installed. */
function cliDescription(cli: UpdateCLIStatus): string {
  const parts: string[] = [];
  if (cli.warnings?.length) parts.push(cli.warnings.join(" · "));
  parts.push(cli.versionManager ? `${cli.method} via ${cli.versionManager}` : cli.method);
  if (cli.selfManaged) {
    parts.push("updates itself");
  } else if (cli.updateCmd) {
    parts.push(cli.updateCmd);
  }
  if (cli.lastRan && cli.installed && cli.lastRan !== cli.installed) {
    parts.push(`last session ran ${cli.lastRan}`);
  }
  return parts.join(" · ");
}

function Value({ children }: { children: React.ReactNode }) {
  return <span className="font-mono text-[12px] text-muted-foreground">{children}</span>;
}

/**
 * What the version check found, in one line. Deliberately never an alarm: a
 * dev build says so and stops, a failed check keeps the last answer and dates
 * it, and an unverified platform says the upgrade is manual rather than
 * offering something that cannot work.
 */
function LatestValue({ status, error }: { status?: UpdateStatus; error?: string }) {
  if (!status) return <Value>checking…</Value>;
  if (status.channel === "dev") {
    return <Value>dev build — not tracked</Value>;
  }

  const age = checkedAgo(status.checkedAt);
  const suffix = [status.checkError ? `check failed${age ? ` · as of ${age}` : ""}` : age, error]
    .filter(Boolean)
    .join(" · ");

  if (!status.latest) {
    return <Value>{suffix || "unknown"}</Value>;
  }

  return (
    <span className="flex items-baseline gap-2">
      <span
        className={cn(
          "font-mono text-[12px]",
          status.behind ? "font-semibold text-foreground-bright" : "text-muted-foreground",
        )}
      >
        {status.latest}
      </span>
      <span className="text-[11px] text-muted-foreground-faint">
        {status.behind ? (status.supported ? "update available" : "manual upgrade") : "up to date"}
        {suffix ? ` · ${suffix}` : ""}
      </span>
    </span>
  );
}

export function AboutSettings() {
  const version = useFeatureStore((s) => s.version);
  const machineId = useFeatureStore((s) => s.machineId);
  const machineLabel = useFeatureStore((s) => s.machineLabel);
  const features = useFeatureStore((s) => s.features);

  const machines = useMachineStore((s) => s.machines);
  const versions = useMachineStore((s) => s.versions);
  const statuses = useMachineStore((s) => s.statuses);
  const lastSeenAt = useMachineStore((s) => s.lastSeenAt);

  // Polling is useUpdateChecks' job (mounted at the root); this panel only
  // reads what it collected, plus an explicit "check now".
  const updates = useUpdateStore((s) => s.statuses);
  const status = updates[PRIMARY_MACHINE_KEY];
  const checking = useUpdateStore((s) => !!s.checking[PRIMARY_MACHINE_KEY]);
  const error = useUpdateStore((s) => s.errors[PRIMARY_MACHINE_KEY]);
  const fetchStatus = useUpdateStore((s) => s.fetch);

  const paired = Object.values(machines);

  return (
    <div className="flex flex-col gap-7">
      <SettingsSection
        title="Build"
        action={
          <Button
            size="sm"
            variant="ghost"
            disabled={checking}
            onClick={() => void fetchStatus(PRIMARY_MACHINE_KEY, true)}
          >
            <RefreshCw className={cn("size-3.5", checking && "animate-spin")} />
            Check now
          </Button>
        }
      >
        <div className="flex flex-col gap-2">
          <SettingsRow label="Version" control={<Value>{version || "unknown"}</Value>} />
          <SettingsRow
            label="Latest release"
            description={status?.releaseUrl ? undefined : "Checked hourly against GitHub."}
            control={<LatestValue status={status} error={error} />}
          />
          <SettingsRow
            label="Machine"
            description="Identity is the id; the name is only presentation."
            control={<Value>{machineLabel || "—"}</Value>}
          />
          <SettingsRow label="Machine id" control={<Value>{machineId || "—"}</Value>} />
        </div>
      </SettingsSection>

      {(status?.clis ?? []).length > 0 && (
        <SettingsSection
          title="Command-line tools"
          description="The binaries this machine would spawn for its next session — not whatever a shell resolves."
        >
          <div className="flex flex-col gap-2">
            {(status?.clis ?? []).map((cli) => (
              <SettingsRow
                key={cli.tool}
                label={cli.tool}
                description={cliDescription(cli)}
                control={<Value>{cli.installed || "version unreadable"}</Value>}
              />
            ))}
          </div>
        </SettingsSection>
      )}

      {paired.length > 0 && (
        <SettingsSection
          title="Machines"
          description="Versions drift independently — each machine upgrades itself."
        >
          <div className="flex flex-col gap-2">
            {paired.map((entry) => {
              const away = statuses[entry.machineId] !== "connected";
              const seen = lastSeenAt[entry.machineId];
              return (
                <SettingsRow
                  key={entry.machineId}
                  label={entry.label || entry.machineId.slice(0, 8)}
                  description={
                    away
                      ? `away${seen ? ` · last seen ${relativeTime(new Date(seen).toISOString())}` : ""}`
                      : undefined
                  }
                  control={
                    <span className={cn(away && "opacity-60")}>
                      <Value>
                        {updates[entry.machineId]?.current ||
                          versions[entry.machineId] ||
                          "unknown"}
                      </Value>
                    </span>
                  }
                />
              );
            })}
          </div>
        </SettingsSection>
      )}

      <SettingsSection
        title="Experimental features"
        description="Enabled in config.toml on this machine; restart to change."
      >
        <div className="flex flex-col gap-2">
          <SettingsRow
            label="Browser panel"
            control={<Value>{features.browser ? "on" : "off"}</Value>}
          />
          <SettingsRow
            label="Teams and personas"
            control={<Value>{features.teams ? "on" : "off"}</Value>}
          />
        </div>
      </SettingsSection>

      <SettingsSection title="Data">
        <SettingsRow
          label="Disk and worktrees"
          description="Sizes, stale worktrees, and backups."
          control={
            <Button asChild size="sm" variant="ghost">
              <Link to="/storage">Open storage</Link>
            </Button>
          }
        />
      </SettingsSection>
    </div>
  );
}
