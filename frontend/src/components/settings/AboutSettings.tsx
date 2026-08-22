/** Settings › About — what this build is and what it has switched on. */
import { Link } from "@tanstack/react-router";
import { SettingsRow, SettingsSection } from "~/components/settings/SettingsLayout";
import { Button } from "~/components/ui/button";
import { useFeatureStore } from "~/stores/feature-store";

function Value({ children }: { children: React.ReactNode }) {
  return <span className="font-mono text-[12px] text-muted-foreground">{children}</span>;
}

export function AboutSettings() {
  const version = useFeatureStore((s) => s.version);
  const machineId = useFeatureStore((s) => s.machineId);
  const machineLabel = useFeatureStore((s) => s.machineLabel);
  const features = useFeatureStore((s) => s.features);

  return (
    <div className="flex flex-col gap-7">
      <SettingsSection title="Build">
        <div className="flex flex-col gap-2">
          <SettingsRow label="Version" control={<Value>{version || "unknown"}</Value>} />
          <SettingsRow
            label="Machine"
            description="Identity is the id; the name is only presentation."
            control={<Value>{machineLabel || "—"}</Value>}
          />
          <SettingsRow label="Machine id" control={<Value>{machineId || "—"}</Value>} />
        </div>
      </SettingsSection>

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
