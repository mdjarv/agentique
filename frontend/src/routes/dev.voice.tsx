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
import { MicMeter } from "~/components/voice/MicMeter";
import { cn } from "~/lib/utils";
import { primaryVoiceUrl } from "~/lib/voice/call";
import { useVoiceStore } from "~/stores/voice-store";

export const Route = createFileRoute("/dev/voice")({
  component: DevVoice,
});

const STATE_COPY: Record<string, { label: string; tone: string }> = {
  idle: { label: "Not connected", tone: "text-muted-foreground" },
  connecting: { label: "Connecting", tone: "text-muted-foreground" },
  live: { label: "Live — speak and you should hear yourself", tone: "text-agent" },
  error: { label: "Failed", tone: "text-destructive" },
  ended: { label: "Call ended", tone: "text-muted-foreground" },
};

function DevVoice() {
  // The same call the rest of the app uses — there is only one, and a second
  // one opened from here would fight the dock over the microphone.
  const status = useVoiceStore((s) => s.status);
  const detail = useVoiceStore((s) => s.detail);
  const log = useVoiceStore((s) => s.log);
  const start = useVoiceStore((s) => s.start);
  const stop = useVoiceStore((s) => s.stop);
  const running = status === "live" || status === "connecting";
  const copy = STATE_COPY[status] ?? STATE_COPY.idle;

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
            onClick={running ? stop : () => start()}
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
          {/* The audio path's own readout: if this does not move while you
              talk, the fault is upstream of the socket. */}
          <MicMeter live={status === "live"} className="ml-auto" />
        </div>

        {detail && <p className="text-sm text-destructive">{detail}</p>}

        <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <dt>Endpoint</dt>
          <dd className="font-mono break-all">{primaryVoiceUrl()}</dd>
          <dt>Capture</dt>
          <dd className="font-mono">16000 Hz · mono · s16le · ~32 ms frames</dd>
        </dl>
      </div>

      {log.length > 0 && (
        <section className="flex flex-col gap-2">
          <h2 className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            Call log
          </h2>
          <ul className="flex flex-col gap-1 text-sm">
            {log.map((entry) => (
              <li key={entry.id} className="flex gap-2">
                <span className="shrink-0 text-muted-foreground">{entry.source}</span>
                <span>{entry.text}</span>
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
