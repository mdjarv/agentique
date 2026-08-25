/**
 * Voice audio-path check.
 *
 * With the server's [voice] backend on `echo`, this talks to a loopback: every
 * frame the microphone captures comes straight back and plays. That exercises
 * capture, worklet batching, framing, upload, download and playback scheduling
 * with no credentials and no model, so a fault here is a browser fault.
 *
 * It is deliberately a bare page rather than a composer affordance. The Android
 * gate — screen behaviour, audio focus, and above all echo cancellation over
 * car Bluetooth — wants one control and a clear readout, tested while the only
 * moving part is an echo.
 *
 * Wear headphones on a desktop, or the echo feeds back.
 */
import { createFileRoute } from "@tanstack/react-router";
import { useVoiceCall } from "~/hooks/useVoiceCall";
import { cn } from "~/lib/utils";
import { primaryVoiceUrl } from "~/lib/voice/call";

export const Route = createFileRoute("/dev/voice")({
  component: DevVoice,
});

const STATE_COPY: Record<string, { label: string; tone: string }> = {
  idle: { label: "Not connected", tone: "text-muted-foreground" },
  connecting: { label: "Connecting", tone: "text-muted-foreground" },
  live: { label: "Live — speak and you should hear yourself", tone: "text-agent" },
  closed: { label: "Call ended", tone: "text-muted-foreground" },
  failed: { label: "Failed", tone: "text-destructive" },
};

function DevVoice() {
  const { state, detail, transcripts, start, stop } = useVoiceCall();
  const running = state === "live" || state === "connecting";
  const copy = STATE_COPY[state] ?? STATE_COPY.idle;

  return (
    <div className="mx-auto flex max-w-xl flex-col gap-6 p-8">
      <header className="flex flex-col gap-2">
        <h1 className="text-lg font-semibold">Voice audio path</h1>
        <p className="text-sm text-muted-foreground">
          Loopback check for capture, framing and playback. Use headphones on a desktop — the echo
          will feed back through open speakers.
        </p>
      </header>

      <div className="flex flex-col gap-3 rounded-xl border bg-agent/5 p-5">
        <div className="flex items-center gap-3">
          <button
            type="button"
            onClick={running ? stop : start}
            className={cn(
              "h-10 rounded-lg px-4 text-sm font-medium transition-colors cursor-pointer",
              running
                ? "bg-destructive/10 text-destructive hover:bg-destructive/20"
                : "bg-agent text-background hover:bg-agent/90",
            )}
          >
            {running ? "End call" : "Start call"}
          </button>
          <span className={cn("text-sm", copy?.tone)}>{copy?.label}</span>
        </div>

        {detail && <p className="text-sm text-destructive">{detail}</p>}

        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <dt>Endpoint</dt>
          <dd className="font-mono break-all">{primaryVoiceUrl()}</dd>
          <dt>Capture</dt>
          <dd className="font-mono">16000 Hz · mono · s16le · ~32 ms frames</dd>
        </dl>
      </div>

      {transcripts.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Transcript
          </h2>
          <ul className="flex flex-col gap-1 text-sm">
            {transcripts.map((t) => (
              <li key={t.id} className="flex gap-2">
                <span className="text-muted-foreground">{t.source ?? "?"}</span>
                <span>{t.text}</span>
              </li>
            ))}
          </ul>
        </section>
      )}

      <p className="text-xs text-muted-foreground">
        The echo engine returns audio at the capture rate; a speech backend returns it at its own.
        The browser reads the rate from the server's <code>ready</code> frame rather than assuming
        one, so this page plays both correctly.
      </p>
    </div>
  );
}
