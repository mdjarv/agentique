import { useMemo, useState } from "react";
import { toast } from "sonner";
import { cronToHuman } from "~/components/schedules/schedule-format";
import { Button } from "~/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "~/components/ui/dialog";
import { Input } from "~/components/ui/input";
import { Label } from "~/components/ui/label";
import { Textarea } from "~/components/ui/textarea";
import { useWebSocket } from "~/hooks/useWebSocket";
import type { ScheduleInfo } from "~/lib/schedule-actions";
import { createSchedule, updateSchedule } from "~/lib/schedule-actions";
import { cn, getErrorMessage } from "~/lib/utils";
import { useAppStore } from "~/stores/app-store";
import { useChatStore } from "~/stores/chat-store";
import { useScheduleStore } from "~/stores/schedule-store";

// Cadence editors: preset (common intervals → constructed cron), raw cron with a
// live human preview, or a one-shot "once at" datetime. Presets that need a
// time-of-day (daily/weekdays) show a time input.
type CadenceTab = "preset" | "cron" | "once";

type PresetId = "15m" | "30m" | "hourly" | "6h" | "daily" | "weekdays";

const PRESETS: { id: PresetId; label: string; needsTime: boolean }[] = [
  { id: "15m", label: "Every 15 min", needsTime: false },
  { id: "30m", label: "Every 30 min", needsTime: false },
  { id: "hourly", label: "Every hour", needsTime: false },
  { id: "6h", label: "Every 6 hours", needsTime: false },
  { id: "daily", label: "Daily at…", needsTime: true },
  { id: "weekdays", label: "Weekdays at…", needsTime: true },
];

function presetToCron(preset: PresetId, time: string): string {
  // Empty/NaN time parts must not silently become 0:00 (midnight) — fall back
  // to 09:00 instead. canSubmit separately blocks submitting an empty time.
  const [hRaw = "", mRaw = ""] = time.split(":");
  const hNum = Number(hRaw);
  const mNum = Number(mRaw);
  const h = String(hRaw !== "" && !Number.isNaN(hNum) ? hNum : 9);
  const m = String(mRaw !== "" && !Number.isNaN(mNum) ? mNum : 0);
  switch (preset) {
    case "15m":
      return "*/15 * * * *";
    case "30m":
      return "*/30 * * * *";
    case "hourly":
      return "0 * * * *";
    case "6h":
      return "0 */6 * * *";
    case "daily":
      return `${m} ${h} * * *`;
    case "weekdays":
      return `${m} ${h} * * 1-5`;
  }
}

/** Heuristic: does this cron fire more often than every 15 minutes? Non-blocking warning only. */
function cronBelow15m(cron: string): boolean {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return false;
  const [min = "", hour = ""] = parts;
  if (hour !== "*") return false;
  if (min === "*") return true;
  const step = min.match(/^\*\/(\d+)$/);
  if (step) return Number(step[1]) < 15;
  if (min.includes(",")) {
    const vals = min.split(",").map(Number);
    // >4 fixed minutes per hour guarantees some gap below 15 minutes.
    if (vals.every((v) => !Number.isNaN(v))) return vals.length > 4;
  }
  return false;
}

/** "YYYY-MM-DDTHH:MM[:SS]" (datetime-local) → RFC3339 with the local UTC offset. */
function localInputToRFC3339(value: string): string {
  if (!value) return "";
  const d = new Date(value); // datetime-local parses as local time
  if (Number.isNaN(d.getTime())) return "";
  const withSeconds = value.length === 16 ? `${value}:00` : value;
  const tzMin = -d.getTimezoneOffset();
  const sign = tzMin >= 0 ? "+" : "-";
  const abs = Math.abs(tzMin);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${withSeconds}${sign}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
}

interface SessionOption {
  id: string;
  name: string;
  projectId: string;
}

export function ScheduleFormDialog({
  schedule,
  onClose,
}: {
  /** When set, the dialog edits this schedule (name/prompt/cron); otherwise it creates one. */
  schedule?: ScheduleInfo;
  onClose: () => void;
}) {
  const ws = useWebSocket();
  const isEdit = !!schedule;
  // Once/dynamic schedules have no editable cadence (update only accepts cron).
  const cadenceEditable = !schedule || schedule.mode === "recurring";

  const sessionsMap = useChatStore((s) => s.sessions);
  const projects = useAppStore((s) => s.projects);

  const [name, setName] = useState(schedule?.name ?? "");
  const [prompt, setPrompt] = useState(schedule?.prompt ?? "");
  const [sessionId, setSessionId] = useState(schedule?.sessionId ?? "");
  const [tab, setTab] = useState<CadenceTab>(isEdit ? "cron" : "preset");
  const [preset, setPreset] = useState<PresetId>("30m");
  const [presetTime, setPresetTime] = useState("09:00");
  const [rawCron, setRawCron] = useState(schedule?.cron ?? "");
  const [onceAt, setOnceAt] = useState("");
  const [serverError, setServerError] = useState("");
  const [saving, setSaving] = useState(false);

  const sessionOptions = useMemo(() => {
    const byProject = new Map<string, SessionOption[]>();
    for (const data of Object.values(sessionsMap)) {
      const meta = data.meta;
      if (meta.archivedAt) continue;
      const list = byProject.get(meta.projectId) ?? [];
      list.push({ id: meta.id, name: meta.name || meta.id.slice(0, 8), projectId: meta.projectId });
      byProject.set(meta.projectId, list);
    }
    const groups = [...byProject.entries()].map(([projectId, sessions]) => ({
      projectId,
      projectName: projects.find((p) => p.id === projectId)?.name ?? projectId.slice(0, 8),
      sessions: sessions.sort((a, b) => a.name.localeCompare(b.name)),
    }));
    groups.sort((a, b) => a.projectName.localeCompare(b.projectName));
    return groups;
  }, [sessionsMap, projects]);

  const effectiveCron =
    tab === "preset" ? presetToCron(preset, presetTime) : tab === "cron" ? rawCron.trim() : "";
  const showFloodWarning = tab !== "once" && !!effectiveCron && cronBelow15m(effectiveCron);

  const selectedPreset = PRESETS.find((p) => p.id === preset);
  // A time-of-day preset with an empty time input must not submit (it would
  // silently fall back to the placeholder time).
  const presetTimeOk = tab !== "preset" || !selectedPreset?.needsTime || presetTime.trim() !== "";

  const canSubmit =
    !saving &&
    name.trim() !== "" &&
    prompt.trim() !== "" &&
    (isEdit || sessionId !== "") &&
    (!cadenceEditable ||
      (tab === "once" ? onceAt !== "" : presetTimeOk && effectiveCron.split(/\s+/).length === 5));

  const handleSubmit = async () => {
    setServerError("");
    setSaving(true);
    try {
      if (isEdit && schedule) {
        const updated = await updateSchedule(ws, {
          id: schedule.id,
          name: name.trim(),
          prompt: prompt.trim(),
          ...(cadenceEditable ? { cron: effectiveCron } : {}),
        });
        useScheduleStore.getState().upsertSchedule(updated);
        toast.success(`Updated "${updated.name}"`);
      } else {
        const projectId = sessionsMap[sessionId]?.meta.projectId;
        if (!projectId) {
          setServerError("Pick a target session first.");
          return;
        }
        const created = await createSchedule(ws, {
          projectId,
          sessionId,
          name: name.trim(),
          prompt: prompt.trim(),
          ...(tab === "once" ? { at: localInputToRFC3339(onceAt) } : { cron: effectiveCron }),
        });
        useScheduleStore.getState().upsertSchedule(created);
        toast.success(`Created "${created.name}"`);
      }
      onClose();
    } catch (err) {
      // Backend validation messages (e.g. "cadence 1m0s is below the 1m0s floor")
      // belong in the dialog so the user can fix the field and retry.
      setServerError(getErrorMessage(err, "Failed to save schedule"));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>{isEdit ? "Edit schedule" : "New schedule"}</DialogTitle>
          {!isEdit && (
            <DialogDescription>
              Re-run a prompt on a session automatically, on a cadence or once at a set time.
            </DialogDescription>
          )}
        </DialogHeader>

        <div className="space-y-4">
          <div className="space-y-1.5">
            <Label htmlFor="schedule-name">Name</Label>
            <Input
              id="schedule-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Deploy check"
              autoFocus
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="schedule-prompt">Prompt</Label>
            <Textarea
              id="schedule-prompt"
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              placeholder="Check the deploy status and report anything unusual."
              rows={4}
            />
          </div>

          <div className="space-y-1.5">
            <Label htmlFor="schedule-session">Target session</Label>
            <select
              id="schedule-session"
              value={sessionId}
              onChange={(e) => setSessionId(e.target.value)}
              disabled={isEdit}
              className="w-full h-9 rounded-md border bg-transparent px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring disabled:opacity-60"
            >
              <option value="" disabled>
                Pick a session…
              </option>
              {isEdit && schedule && !sessionsMap[schedule.sessionId] && (
                <option value={schedule.sessionId}>{schedule.sessionId.slice(0, 8)}</option>
              )}
              {sessionOptions.map((group) => (
                <optgroup key={group.projectId} label={group.projectName}>
                  {group.sessions.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </div>

          {cadenceEditable ? (
            <div className="space-y-2">
              <Label>Cadence</Label>
              <div className="flex gap-1 rounded-md border p-0.5 w-fit">
                {(
                  [
                    ["preset", "Preset"],
                    ["cron", "Cron"],
                    ...(isEdit ? [] : [["once", "Once at"] as const]),
                  ] as [CadenceTab, string][]
                ).map(([id, label]) => (
                  <button
                    key={id}
                    type="button"
                    onClick={() => setTab(id)}
                    className={cn(
                      "px-2.5 py-1 text-xs rounded transition-colors",
                      tab === id
                        ? "bg-primary/10 text-primary font-medium"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {label}
                  </button>
                ))}
              </div>

              {tab === "preset" && (
                <div className="flex flex-wrap items-center gap-2">
                  <select
                    value={preset}
                    onChange={(e) => setPreset(e.target.value as PresetId)}
                    className="h-9 rounded-md border bg-transparent px-3 text-sm focus:outline-none focus:ring-1 focus:ring-ring"
                  >
                    {PRESETS.map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.label}
                      </option>
                    ))}
                  </select>
                  {selectedPreset?.needsTime && (
                    <Input
                      type="time"
                      value={presetTime}
                      onChange={(e) => setPresetTime(e.target.value)}
                      className="w-28"
                    />
                  )}
                </div>
              )}

              {tab === "cron" && (
                <div className="space-y-1">
                  <Input
                    value={rawCron}
                    onChange={(e) => setRawCron(e.target.value)}
                    placeholder="*/30 * * * *"
                    className="font-mono"
                  />
                  {rawCron.trim() && (
                    <p className="text-xs text-muted-foreground">{cronToHuman(rawCron.trim())}</p>
                  )}
                </div>
              )}

              {tab === "once" && (
                <Input
                  type="datetime-local"
                  value={onceAt}
                  onChange={(e) => setOnceAt(e.target.value)}
                  className="w-fit"
                />
              )}

              {showFloodWarning && (
                <p className="text-xs text-amber-600 dark:text-amber-400">
                  Sub-15m cadences flood the session timeline — consider a longer interval.
                </p>
              )}
            </div>
          ) : (
            <p className="text-xs text-muted-foreground">
              {schedule?.mode === "once"
                ? "One-shot schedule — the fire time can't be changed."
                : "Self-paced schedule — the agent sets its own next run."}
            </p>
          )}

          {serverError && <p className="text-sm text-destructive">{serverError}</p>}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose} disabled={saving}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={!canSubmit}>
            {isEdit ? "Save" : "Create"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
