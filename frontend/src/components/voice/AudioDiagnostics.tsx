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
 *
 * **The parts render content and never their own headings.** The fault this
 * diagnoses is a phone in a car, and on that phone the app is installed as a
 * PWA with no address bar — so its real home is Settings → Voice, reachable by
 * tapping, and `/dev/voice` is the desktop copy. Two hosts, two kinds of
 * chrome, one set of words: [AUDIO_CHECK_COPY] is read by both so the page you
 * can reach and the page you can type cannot describe the same check
 * differently.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { cn } from "~/lib/utils";
import { PROBE_COPY, type ProbePath, type RunningProbe, startProbe } from "~/lib/voice/audio-check";
import {
  type AudioRoute,
  DISTANCE_COPY,
  listOutputs,
  NO_ROUTE,
  type OutputDevices,
  PROFILE_COPY,
  readDistance,
  readProfile,
} from "~/lib/voice/audio-route";
import type { CallAudioReport } from "~/lib/voice/call";
import { callAudioReport, useVoiceStore } from "~/stores/voice-store";

const PROBE_ORDER: ProbePath[] = ["call", "buffered", "element"];

/** What each part is called and what it is for, wherever it is hosted. */
export const AUDIO_CHECK_COPY = {
  probes: {
    title: "Can you hear this?",
    description:
      "One tone, three ways to send it. They differ by one thing each, so the one you hear is the one to ship. Play them where the fault is — in the car, on the connection that was silent.",
  },
  routes: {
    title: "Where this call's audio went",
    description:
      "Three readings of one output. A difference between the first two is the route moving when the microphone opened — the fault this subsystem has always suspected and never been able to show.",
  },
} as const;

/** What a probe left behind, whether it is still sounding or done. */
interface ProbeState {
  /** Still making a noise. */
  playing: boolean;
  /** Seconds it has been sounding. */
  elapsed: number;
  /** Set once the browser has ruled, and only when it ruled against. */
  detail: string;
  route: AudioRoute;
}

/**
 * Press, listen, read.
 *
 * **A probe plays until it is stopped**, and the button says so. A sink that
 * takes most of a second to wake — which is every Bluetooth and projection
 * route — swallows the beginning of a stream, so a tone with a fixed duration
 * can be inaudible on a path that works perfectly. Holding it open moves that
 * decision to the ear that is listening, and the elapsed seconds are shown
 * because "I heard it after about two seconds" is itself the finding.
 *
 * The route is re-read every second while it sounds, for the same reason the
 * call keeps two readings: a route that *moves* is what is being hunted, and it
 * moves at a moment nobody is holding a stopwatch for.
 *
 * One probe at a time: two contexts making noise at once would say nothing
 * about which one was heard, which is the only thing being asked. They are
 * refused while a call is up, because the call owns the route and a probe that
 * moves it mid-call has changed the thing it measures.
 */
export function OutputProbes() {
  const status = useVoiceStore((s) => s.status);
  const callUp = status === "connecting" || status === "live";
  const [active, setActive] = useState<ProbePath | null>(null);
  const [states, setStates] = useState<Partial<Record<ProbePath, ProbeState>>>({});
  const probe = useRef<RunningProbe | null>(null);

  // Stable, so `stop` below is stable, so the unmount effect that calls it does
  // not re-run on every render — which would stop the tone the moment anything
  // else on the page changed.
  const update = useCallback((path: ProbePath, patch: Partial<ProbeState>) => {
    setStates((prev) => ({
      ...prev,
      [path]: { playing: false, elapsed: 0, detail: "", route: NO_ROUTE, ...prev[path], ...patch },
    }));
  }, []);

  const stop = useCallback(() => {
    const running = probe.current;
    if (!running) return;
    probe.current = null;
    setActive(null);
    update(running.path, { playing: false, route: running.route() });
    void running.stop();
  }, [update]);

  const start = (path: ProbePath) => {
    if (probe.current) return;
    // Reached synchronously from this handler and it must stay that way: the
    // context it builds is only allowed to make a sound because this click is
    // still the browser's idea of a user gesture.
    const running = startProbe(path, stop);
    probe.current = running;
    setActive(path);
    update(path, { playing: true, elapsed: 0, detail: "", route: running.route() });
    void running.ready.then((ready) => {
      if (probe.current !== running) return;
      update(path, { detail: ready.ok ? "" : ready.detail, route: running.route() });
    });
  };

  // The tick is the probe's, so nothing renders on a second when nothing is
  // playing, and it stops with the probe rather than outliving it.
  useEffect(() => {
    if (!active) return;
    const timer = setInterval(() => {
      const running = probe.current;
      if (!running) return;
      setStates((prev) => {
        const previous = prev[active];
        if (!previous) return prev;
        return {
          ...prev,
          [active]: { ...previous, elapsed: previous.elapsed + 1, route: running.route() },
        };
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [active]);

  // A probe must never outlive the surface that started it: navigating away
  // from a page still making a noise is how a tone ends up playing over the
  // call the operator went to start.
  useEffect(() => stop, [stop]);

  // The call owns the route, so it wins.
  useEffect(() => {
    if (callUp) stop();
  }, [callUp, stop]);

  return (
    <div className="flex flex-col gap-2">
      {callUp && (
        <p className="text-[12px] text-muted-foreground">
          End the call first — it owns the audio route while it is up.
        </p>
      )}

      <ul className="flex flex-col gap-2">
        {PROBE_ORDER.map((path) => {
          const copy = PROBE_COPY[path];
          const state = states[path];
          const playing = active === path;
          const blocked = callUp || (active !== null && !playing);
          return (
            <li key={path} className="rounded-lg border border-border/60 bg-card p-3">
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  disabled={blocked}
                  onClick={() => (playing ? stop() : start(path))}
                  className={cn(
                    "h-9 w-16 shrink-0 rounded-md text-xs font-medium transition-colors",
                    playing
                      ? "bg-destructive/15 text-destructive hover:bg-destructive/25"
                      : "bg-agent text-background hover:bg-agent/90",
                    "disabled:cursor-not-allowed disabled:opacity-40",
                    !blocked && "cursor-pointer",
                  )}
                >
                  {playing ? "Stop" : "Play"}
                </button>
                <span className="flex min-w-0 flex-col">
                  <span className="flex items-baseline gap-2">
                    <span className="text-[12.5px] font-medium">{copy.title}</span>
                    {playing && (
                      <span className="shrink-0 font-mono text-[11px] text-agent">
                        {state?.elapsed ?? 0}s
                      </span>
                    )}
                  </span>
                  <span className="text-[11.5px] text-muted-foreground">{copy.detail}</span>
                </span>
              </div>
              {state && (state.detail || state.route.state !== "none") && (
                <div className="mt-2 border-t border-border/60 pt-2">
                  {state.detail && (
                    <p className="mb-1 text-[11px] text-destructive">
                      It never played: {state.detail}
                    </p>
                  )}
                  <RouteFacts route={state.route} />
                </div>
              )}
            </li>
          );
        })}
      </ul>

      <p className="px-1 text-[11.5px] text-muted-foreground-faint">
        It keeps sounding until you stop it. A Bluetooth or car route can take a second to wake, and
        a short beep on one of those is silent even when the path works.
      </p>

      <OutputDeviceNote />
    </div>
  );
}

/**
 * The live call's own route, at the three moments worth comparing.
 *
 * Polled rather than pushed, and only while there is a call to poll: this is
 * shown on two pages nobody watches during a drive, so putting it in the store
 * would re-render every call surface once a second for nobody's benefit.
 *
 * It renders its own "no call" line rather than disappearing. A section that
 * vanishes reads as a page that is missing something; a line saying the call is
 * not running says what to do next.
 */
export function CallRoutes() {
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

  if (!report) {
    return (
      <p className="text-[12px] text-muted-foreground">
        No call is running. Start one and come back — the call survives leaving this page.
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-2">
      <RouteMoment label="When you pressed call" route={report.placed} />
      <RouteMoment label="Once the microphone opened" route={report.live} />
      <RouteMoment label="Now" route={report.now} />

      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 px-1 text-[11.5px] text-muted-foreground">
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
    </div>
  );
}

function RouteMoment({ label, route }: { label: string; route: AudioRoute }) {
  return (
    <div className="rounded-lg border border-border/60 bg-card p-3">
      <p className="mb-1 text-[12.5px] font-medium">{label}</p>
      {route.state === "none" ? (
        <p className="text-[11.5px] text-muted-foreground">No output was open.</p>
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
 * A footnote under the probes rather than a section of its own, because it is
 * one sentence in the case that matters and it is *about* the probes: an empty
 * list is the finding, not a failure. Android Chrome enumerates no outputs and
 * does not implement `setSinkId`, so "none reported" is this app saying
 * precisely that it has no way to move the audio itself — and that whatever
 * fixes a silent car is a different route, not a chosen device.
 */
function OutputDeviceNote() {
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

  if (!outputs.supported) {
    return (
      <p className="px-1 text-[11.5px] text-muted-foreground-faint">
        This browser does not enumerate audio devices at all.
      </p>
    );
  }

  if (outputs.devices.length === 0) {
    return (
      <p className="px-1 text-[11.5px] text-muted-foreground-faint">
        This browser offers no output devices to choose from — normal on Android. The route is the
        platform's to pick, not ours.
      </p>
    );
  }

  return (
    <p className="px-1 text-[11.5px] text-muted-foreground-faint">
      Outputs offered: {outputs.devices.map((d) => d.label || "unnamed").join(", ")}
    </p>
  );
}

/** Seconds as milliseconds, with the unreported case said rather than shown as 0. */
function formatSeconds(seconds: number): string {
  if (seconds < 0) return "not reported";
  return `${Math.round(seconds * 1000)} ms`;
}

/**
 * The desktop composition, for `/dev/voice`.
 *
 * Settings composes the same two parts with its own section chrome; the words
 * come from [AUDIO_CHECK_COPY] either way.
 */
export function AudioDiagnostics() {
  return (
    <div className="flex flex-col gap-6">
      <section className="flex flex-col gap-3">
        <header className="flex flex-col gap-1">
          <h2 className="text-sm font-semibold">{AUDIO_CHECK_COPY.probes.title}</h2>
          <p className="text-xs text-muted-foreground">{AUDIO_CHECK_COPY.probes.description}</p>
        </header>
        <OutputProbes />
      </section>

      <section className="flex flex-col gap-3">
        <header className="flex flex-col gap-1">
          <h2 className="text-sm font-semibold">{AUDIO_CHECK_COPY.routes.title}</h2>
          <p className="text-xs text-muted-foreground">{AUDIO_CHECK_COPY.routes.description}</p>
        </header>
        <CallRoutes />
      </section>
    </div>
  );
}
