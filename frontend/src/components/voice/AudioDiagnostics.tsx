/**
 * The readout for a call nobody could hear.
 *
 * It exists because of one field report and the shape of it: live voice worked
 * over a wired Android Auto connection and was silent over a wireless one, with
 * transcripts arriving, no warning on the status line, and every health signal
 * the app has saying the audio path was fine. It was fine. The audio was going
 * somewhere else, and nothing in the app could say where.
 *
 * So this is two questions, side by side, and neither is "is it working".
 *
 * **Can this car play a tone at all, and which kind?** Three probes, one
 * variable apart, answered by ear in the time it takes to press them. That is
 * the diagnosis: hearing the second or third and not the first names the fix.
 *
 * **Where did the call's audio actually go?** The route as it was in the
 * gesture, as it was once the microphone opened, and as it is now — because the
 * standing suspicion in this subsystem is a route that moves when the
 * microphone opens, and a single reading cannot show a move.
 *
 * The operator's ear is the measurement. Everything on screen only says where
 * the sound was sent, which is the half they cannot hear.
 */
import { useEffect, useState } from "react";
import { cn } from "~/lib/utils";
import { PROBE_COPY, type ProbePath, type ProbeResult, probeOutput } from "~/lib/voice/audio-check";
import {
  type AudioRoute,
  DISTANCE_COPY,
  listOutputs,
  type OutputDevices,
  PROFILE_COPY,
  readDistance,
  readProfile,
} from "~/lib/voice/audio-route";
import type { CallAudioReport } from "~/lib/voice/call";
import { callAudioReport, useVoiceStore } from "~/stores/voice-store";

const PROBE_ORDER: ProbePath[] = ["call", "buffered", "element"];

export function AudioDiagnostics() {
  return (
    <div className="flex flex-col gap-6">
      <OutputProbes />
      <CallRoutes />
      <OutputDeviceList />
    </div>
  );
}

/**
 * Press, listen, read.
 *
 * One probe at a time: two contexts making noise at once would tell you
 * nothing about which one you were hearing, which is the only thing being
 * asked. They are also refused while a call is up, because the call owns the
 * route and a probe that moves it mid-call has changed the thing it measures.
 */
function OutputProbes() {
  const status = useVoiceStore((s) => s.status);
  const callUp = status === "connecting" || status === "live";
  const [running, setRunning] = useState<ProbePath | null>(null);
  const [results, setResults] = useState<Partial<Record<ProbePath, ProbeResult>>>({});

  const run = (path: ProbePath) => {
    if (running) return;
    setRunning(path);
    // The probe is reached synchronously from this handler and must stay that
    // way: the context it builds is only allowed to make a sound because this
    // click is still the browser's idea of a user gesture.
    void probeOutput(path).then((result) => {
      setResults((prev) => ({ ...prev, [path]: result }));
      setRunning(null);
    });
  };

  return (
    <section className="flex flex-col gap-3">
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold">Can you hear this?</h2>
        <p className="text-xs text-muted-foreground">
          Two seconds of tone, three ways to send it. They differ by one thing each, so the one you
          hear is the one to ship. Play them where the fault is — in the car, on the connection that
          was silent.
        </p>
      </header>

      {callUp && (
        <p className="text-xs text-muted-foreground">
          End the call first — it owns the audio route while it is up.
        </p>
      )}

      <ul className="flex flex-col gap-2">
        {PROBE_ORDER.map((path) => {
          const copy = PROBE_COPY[path];
          const result = results[path];
          const busy = running === path;
          return (
            <li key={path} className="rounded-lg border bg-card/40 p-3">
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  disabled={callUp || running !== null}
                  onClick={() => run(path)}
                  className={cn(
                    "h-8 shrink-0 rounded-md px-3 text-xs font-medium transition-colors",
                    "bg-agent text-background hover:bg-agent/90",
                    "disabled:cursor-not-allowed disabled:opacity-40",
                    !callUp && running === null && "cursor-pointer",
                  )}
                >
                  {busy ? "Playing…" : "Play"}
                </button>
                <span className="flex min-w-0 flex-col">
                  <span className="text-xs font-medium">{copy.title}</span>
                  <span className="text-[11px] text-muted-foreground">{copy.detail}</span>
                </span>
              </div>
              {result && (
                <div className="mt-2 border-t pt-2">
                  {!result.started && (
                    <p className="mb-1 text-[11px] text-destructive">
                      It never played{result.detail ? `: ${result.detail}` : ""}
                    </p>
                  )}
                  <RouteFacts route={result.route} />
                </div>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/**
 * The live call's own route, at the three moments worth comparing.
 *
 * Polled rather than pushed, and only while there is a call to poll: this is
 * the one page that shows it, so putting it in the store would re-render every
 * call surface once a second for nobody's benefit.
 */
function CallRoutes() {
  const status = useVoiceStore((s) => s.status);
  const [report, setReport] = useState<CallAudioReport | null>(null);

  useEffect(() => {
    if (status === "idle") {
      setReport(null);
      return;
    }
    const read = () => setReport(callAudioReport());
    read();
    const timer = setInterval(read, 1000);
    return () => clearInterval(timer);
  }, [status]);

  if (!report) return null;

  return (
    <section className="flex flex-col gap-3">
      <header className="flex flex-col gap-1">
        <h2 className="text-sm font-semibold">Where this call's audio went</h2>
        <p className="text-xs text-muted-foreground">
          Three readings of one output. A difference between the first two is the route moving when
          the microphone opened — the fault this subsystem has always suspected and never been able
          to show.
        </p>
      </header>

      <div className="flex flex-col gap-2">
        <RouteMoment label="When you pressed call" route={report.placed} />
        <RouteMoment label="Once the microphone opened" route={report.live} />
        <RouteMoment label="Now" route={report.now} />
      </div>

      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <dt>Health</dt>
        <dd className="font-mono">{report.health}</dd>
        <dt>Audio received</dt>
        <dd className="font-mono">
          {report.pcmBytes.toLocaleString()} bytes at {report.outputSampleRate} Hz
        </dd>
        <dt>Microphone</dt>
        <dd className="font-mono break-all">
          {report.capture.active
            ? `${report.capture.device || "unnamed"} · ${report.capture.contextSampleRate} Hz · echo cancellation ${report.capture.echoCancellation ? "on" : "off"}`
            : "not open"}
        </dd>
      </dl>
    </section>
  );
}

function RouteMoment({ label, route }: { label: string; route: AudioRoute }) {
  return (
    <div className="rounded-lg border bg-card/40 p-3">
      <p className="mb-1 text-xs font-medium">{label}</p>
      {route.state === "none" ? (
        <p className="text-[11px] text-muted-foreground">No output was open.</p>
      ) : (
        <RouteFacts route={route} />
      )}
    </div>
  );
}

/**
 * One route, as numbers and as the two readings they support.
 *
 * The readings are printed as "consistent with", never as a verdict. They come
 * from one threshold each and the operator is standing next to the actual
 * answer, so the numbers are the finding and the words are a hint at what they
 * mean.
 */
function RouteFacts({ route }: { route: AudioRoute }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-0.5 text-[11px] text-muted-foreground">
      <dt>State</dt>
      <dd className="font-mono">{route.state}</dd>
      <dt>Rate</dt>
      <dd className="font-mono">
        {route.sampleRate} Hz — {PROFILE_COPY[readProfile(route)]}
      </dd>
      <dt>Output latency</dt>
      <dd className="font-mono">
        {formatSeconds(route.outputLatency)} — {DISTANCE_COPY[readDistance(route)]}
      </dd>
      <dt>Base latency</dt>
      <dd className="font-mono">{formatSeconds(route.baseLatency)}</dd>
      <dt>Sink</dt>
      <dd className="font-mono break-all">{route.sinkId || "the browser default"}</dd>
    </dl>
  );
}

/**
 * What the browser will admit exists to play through.
 *
 * An empty list is the finding rather than a failure. Android Chrome does not
 * enumerate outputs and does not implement `setSinkId`, so "none reported" is
 * this app saying, precisely, that it has no way to move the audio itself — and
 * that whatever fixes a silent car is a different route, not a chosen device.
 */
function OutputDeviceList() {
  const [outputs, setOutputs] = useState<OutputDevices | null>(null);

  useEffect(() => {
    let live = true;
    void listOutputs().then((found) => {
      if (live) setOutputs(found);
    });
    return () => {
      live = false;
    };
  }, []);

  if (!outputs) return null;

  return (
    <section className="flex flex-col gap-2">
      <h2 className="text-sm font-semibold">Outputs this browser offers</h2>
      {!outputs.supported ? (
        <p className="text-xs text-muted-foreground">
          This browser does not enumerate devices at all.
        </p>
      ) : outputs.devices.length === 0 ? (
        <p className="text-xs text-muted-foreground">
          None reported — normal on Android, where the browser exposes no output selection. The
          route is the platform's to choose, not ours.
        </p>
      ) : (
        <ul className="flex flex-col gap-0.5 font-mono text-[11px] text-muted-foreground">
          {outputs.devices.map((device) => (
            <li key={device.id} className="break-all">
              {device.label || "unnamed"} <span className="opacity-60">({device.id})</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** Seconds as milliseconds, with the unreported case said rather than shown as 0. */
function formatSeconds(seconds: number): string {
  if (seconds < 0) return "not reported";
  return `${Math.round(seconds * 1000)} ms`;
}
