import { Check, Loader2 } from "lucide-react";
import { useCallback, useEffect, useState } from "react";
import { SettingsRow, SettingsSection } from "~/components/settings/SettingsLayout";
import { cn } from "~/lib/utils";
import { useFeatureStore } from "~/stores/feature-store";

interface VoiceOption {
  value: string;
  label?: string;
  hint?: string;
}

interface VoiceSettingsPayload {
  voiceName?: string;
  model?: string;
  personality?: string;
  verbosity?: string;
  configModel?: string;
  voices?: VoiceOption[];
  verbosities?: VoiceOption[];
}

const MAX_PERSONALITY = 1200;

/**
 * How the live voice agent sounds and behaves.
 *
 * These are server-side settings rather than per-browser ones: the persona
 * belongs to the agent, so a call opened from the phone should sound like a
 * call opened from the desktop. They are read at the start of each call, so a
 * change here lands on the next call — no restart.
 */
export function VoiceSettings() {
  const voiceEnabled = useFeatureStore((s) => s.features.voice);
  const [settings, setSettings] = useState<VoiceSettingsPayload | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [savedAt, setSavedAt] = useState(0);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const resp = await fetch("/api/voice/settings");
        if (!resp.ok) throw new Error(`load failed (${resp.status})`);
        const data: VoiceSettingsPayload = await resp.json();
        if (!cancelled) setSettings(data);
      } catch (err) {
        if (!cancelled) setLoadError(err instanceof Error ? err.message : String(err));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const save = useCallback(async (next: VoiceSettingsPayload) => {
    setSaving(true);
    try {
      const resp = await fetch("/api/voice/settings", {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          voiceName: next.voiceName ?? "",
          model: next.model ?? "",
          personality: next.personality ?? "",
          verbosity: next.verbosity ?? "",
        }),
      });
      if (!resp.ok) throw new Error(`save failed (${resp.status})`);
      // Trust the response over the local edit: the server clamps, so this is
      // what a call will actually use.
      setSettings(await resp.json());
      setSavedAt(Date.now());
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  }, []);

  if (loadError && !settings) {
    return (
      <p className="text-[13px] text-destructive">Could not load voice settings: {loadError}</p>
    );
  }
  if (!settings) {
    return (
      <p className="flex items-center gap-2 text-[13px] text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" /> Loading…
      </p>
    );
  }

  const voices = settings.voices ?? [];
  const verbosities = settings.verbosities ?? [];
  const personality = settings.personality ?? "";

  return (
    <div className="flex flex-col gap-8">
      {!voiceEnabled && (
        <p className="rounded-lg border border-border/60 bg-card px-3.5 py-3 text-[12.5px] text-muted-foreground">
          Live voice is off on this machine, so these have nothing to affect yet. Turn it on with{" "}
          <code className="text-foreground">[experimental] voice</code> in{" "}
          <code className="text-foreground">config.toml</code> and restart. What you set here is
          kept either way.
        </p>
      )}

      <SettingsSection
        title="Voice"
        description="How the live agent sounds. Takes effect on your next call."
      >
        <div className="grid grid-cols-[repeat(auto-fill,minmax(8.5rem,1fr))] gap-2">
          {voices.map((option) => {
            const active = (settings.voiceName ?? "") === option.value;
            return (
              <button
                key={option.value || "default"}
                type="button"
                onClick={() => void save({ ...settings, voiceName: option.value })}
                className={cn(
                  "flex flex-col items-start gap-0.5 rounded-lg border px-3 py-2.5 text-left transition-colors cursor-pointer",
                  active
                    ? "border-agent bg-agent/10"
                    : "border-border/60 bg-card hover:border-border hover:bg-muted/40",
                )}
                aria-pressed={active}
              >
                <span className="flex w-full items-center gap-1.5">
                  <span className="truncate text-[13px] font-medium text-foreground">
                    {option.label || option.value}
                  </span>
                  {active && <Check className="ml-auto h-3.5 w-3.5 shrink-0 text-agent" />}
                </span>
                {option.hint && (
                  <span className="truncate text-[11.5px] text-muted-foreground-faint">
                    {option.hint}
                  </span>
                )}
              </button>
            );
          })}
        </div>
        <SettingsRow
          label="Another voice"
          description="Any name the speech backend accepts. New voices work here before they appear above."
          control={
            <TextField
              value={settings.voiceName ?? ""}
              placeholder="e.g. Puck"
              onCommit={(voiceName) => void save({ ...settings, voiceName })}
            />
          }
        />
      </SettingsSection>

      <SettingsSection
        title="Personality"
        description="How it comes across. Tone only — it never changes the read-back or the confirmation."
      >
        <div className="flex flex-col gap-2">
          <div className="grid grid-cols-[repeat(auto-fill,minmax(8.5rem,1fr))] gap-2">
            {verbosities.map((option) => {
              const active = (settings.verbosity ?? "brief") === option.value;
              return (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => void save({ ...settings, verbosity: option.value })}
                  className={cn(
                    "flex flex-col items-start gap-0.5 rounded-lg border px-3 py-2.5 text-left transition-colors cursor-pointer",
                    active
                      ? "border-agent bg-agent/10"
                      : "border-border/60 bg-card hover:border-border hover:bg-muted/40",
                  )}
                  aria-pressed={active}
                >
                  <span className="text-[13px] font-medium text-foreground">{option.label}</span>
                  {option.hint && (
                    <span className="text-[11.5px] text-muted-foreground-faint">{option.hint}</span>
                  )}
                </button>
              );
            })}
          </div>

          <div className="flex flex-col gap-1.5 rounded-lg border border-border/60 bg-card px-3.5 py-3">
            <span className="text-[13px] font-medium text-foreground">Character</span>
            <span className="text-[12px] text-muted-foreground-faint">
              A sentence or two describing how it should come across. Left empty, it is plain and
              matter-of-fact.
            </span>
            <TextArea
              value={personality}
              placeholder="Dry, a bit terse, never chirpy. Gets to the point."
              maxLength={MAX_PERSONALITY}
              onCommit={(next) => void save({ ...settings, personality: next })}
            />
          </div>
        </div>
      </SettingsSection>

      <SettingsSection title="Model" description="The realtime speech model a call connects to.">
        <SettingsRow
          label="Override"
          description={
            settings.configModel
              ? `Empty uses the configured ${settings.configModel}.`
              : "Empty uses the backend's default, so a new upstream model needs no update here."
          }
          control={
            <TextField
              value={settings.model ?? ""}
              placeholder={settings.configModel || "backend default"}
              wide
              onCommit={(model) => void save({ ...settings, model })}
            />
          }
        />
      </SettingsSection>

      <div className="flex h-5 items-center gap-2 text-[12px]">
        {saving && (
          <span className="flex items-center gap-1.5 text-muted-foreground">
            <Loader2 className="h-3 w-3 animate-spin" /> Saving…
          </span>
        )}
        {!saving && savedAt > 0 && (
          <span className="flex items-center gap-1.5 text-agent">
            <Check className="h-3 w-3" /> Saved — your next call will use it.
          </span>
        )}
        {loadError && settings && <span className="text-destructive">{loadError}</span>}
      </div>
    </div>
  );
}

/**
 * A text input that commits on blur or Enter rather than per keystroke.
 *
 * Every commit is a round trip, so committing on change would write once per
 * letter typed.
 */
function TextField({
  value,
  placeholder,
  wide,
  onCommit,
}: {
  value: string;
  placeholder?: string;
  wide?: boolean;
  onCommit: (value: string) => void;
}) {
  const [draft, setDraft] = useState(value);
  // Re-sync when the server answers with something different from the edit.
  useEffect(() => setDraft(value), [value]);

  const commit = () => {
    if (draft !== value) onCommit(draft);
  };

  return (
    <input
      type="text"
      value={draft}
      placeholder={placeholder}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={commit}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur();
        if (e.key === "Escape") setDraft(value);
      }}
      className={cn(
        "rounded-md border border-border/60 bg-background px-2.5 py-1.5 text-[13px] text-foreground placeholder:text-muted-foreground-faint focus:border-agent/50 focus:outline-none focus:ring-1 focus:ring-agent/30",
        wide ? "w-64 max-md:w-40" : "w-40",
      )}
    />
  );
}

function TextArea({
  value,
  placeholder,
  maxLength,
  onCommit,
}: {
  value: string;
  placeholder?: string;
  maxLength: number;
  onCommit: (value: string) => void;
}) {
  const [draft, setDraft] = useState(value);
  useEffect(() => setDraft(value), [value]);

  return (
    <>
      <textarea
        value={draft}
        placeholder={placeholder}
        maxLength={maxLength}
        rows={3}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => {
          if (draft !== value) onCommit(draft);
        }}
        className="mt-1 w-full resize-y rounded-md border border-border/60 bg-background px-2.5 py-2 text-[13px] text-foreground placeholder:text-muted-foreground-faint focus:border-agent/50 focus:outline-none focus:ring-1 focus:ring-agent/30"
      />
      <span className="self-end text-[11px] tabular-nums text-muted-foreground-faint">
        {draft.length}/{maxLength}
      </span>
    </>
  );
}
