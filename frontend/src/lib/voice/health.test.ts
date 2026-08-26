import { describe, expect, it } from "vitest";
import {
  type AudioHealthSample,
  assessAudioHealth,
  HEALTH_MESSAGE,
  type VoiceAudioHealth,
} from "./health";

const NOW = 1_000_000;

/** A live call where everything works. Each case names only what it breaks. */
function healthy(over: Partial<AudioHealthSample> = {}): AudioHealthSample {
  return {
    now: NOW,
    liveSince: NOW - 20_000,
    micSoundAt: NOW - 200,
    engineSpokeAt: NOW - 1000,
    audioFrameAt: NOW - 500,
    audioRunning: true,
    clockAdvancing: true,
    ...over,
  };
}

describe("assessAudioHealth", () => {
  const cases: { name: string; sample: AudioHealthSample; want: VoiceAudioHealth }[] = [
    {
      name: "a working call says nothing",
      sample: healthy(),
      want: "ok",
    },
    {
      name: "a call that is not live yet is never faulted",
      sample: healthy({ liveSince: 0, micSoundAt: 0, audioRunning: false }),
      want: "ok",
    },

    // MIC SILENT — the car case. The model never heard a word, which is what a
    // silent assistant is made of.
    {
      name: "six seconds of live call with a mic that has heard nothing",
      sample: healthy({ liveSince: NOW - 6000, micSoundAt: 0, engineSpokeAt: 0, audioFrameAt: 0 }),
      want: "mic-silent",
    },
    {
      name: "a mic that went quiet six seconds ago",
      sample: healthy({ micSoundAt: NOW - 6000, engineSpokeAt: 0, audioFrameAt: 0 }),
      want: "mic-silent",
    },
    {
      name: "five seconds of silence is a pause, not a fault",
      sample: healthy({ micSoundAt: NOW - 5000, engineSpokeAt: 0, audioFrameAt: 0 }),
      want: "ok",
    },
    {
      name: "a call live for less than the window is still settling",
      sample: healthy({ liveSince: NOW - 3000, micSoundAt: 0, engineSpokeAt: 0, audioFrameAt: 0 }),
      want: "ok",
    },

    // REPLIES NOT ARRIVING — text came, audio did not.
    {
      name: "the assistant spoke and no audio followed",
      sample: healthy({ engineSpokeAt: NOW - 5000, audioFrameAt: 0 }),
      want: "no-audio",
    },
    {
      name: "audio from before the reply does not count as the reply's",
      sample: healthy({ engineSpokeAt: NOW - 6000, audioFrameAt: NOW - 7000 }),
      want: "no-audio",
    },
    {
      name: "audio still has a moment to start arriving",
      sample: healthy({ engineSpokeAt: NOW - 4000, audioFrameAt: 0 }),
      want: "ok",
    },
    {
      name: "audio that arrived after the reply is the reply",
      sample: healthy({ engineSpokeAt: NOW - 6000, audioFrameAt: NOW - 5500 }),
      want: "ok",
    },
    {
      name: "an assistant that has not spoken is not a missing reply",
      sample: healthy({ engineSpokeAt: 0, audioFrameAt: 0 }),
      want: "ok",
    },

    // CANNOT PLAY — the frames are here and the context will not play them.
    {
      name: "a suspended context",
      sample: healthy({ audioRunning: false }),
      want: "cannot-play",
    },
    {
      name: "a context that calls itself running with a frozen clock",
      sample: healthy({ clockAdvancing: false }),
      want: "cannot-play",
    },
    {
      name: "a frozen clock with no audio to play is not a playback fault",
      sample: healthy({ clockAdvancing: false, audioFrameAt: 0, engineSpokeAt: 0 }),
      want: "ok",
    },
    {
      name: "a clock frozen since long after the last frame is nothing to report",
      sample: healthy({ clockAdvancing: false, audioFrameAt: NOW - 30_000, engineSpokeAt: 0 }),
      want: "ok",
    },
  ];

  for (const { name, sample, want } of cases) {
    it(`${name} -> ${want}`, () => {
      expect(assessAudioHealth(sample)).toBe(want);
    });
  }

  // One line, one message. A status line reporting three faults at once
  // reports none of them, so overlap is ranked rather than concatenated.
  describe("ranking, where more than one is true at once", () => {
    it("a broken output path outranks a silent mic, because a gesture fixes it", () => {
      const sample = healthy({
        audioRunning: false,
        liveSince: NOW - 30_000,
        micSoundAt: 0,
      });
      expect(assessAudioHealth(sample)).toBe("cannot-play");
    });

    it("a silent mic outranks a missing reply, because it is the cause", () => {
      const sample = healthy({
        micSoundAt: 0,
        engineSpokeAt: NOW - 10_000,
        audioFrameAt: 0,
      });
      expect(assessAudioHealth(sample)).toBe("mic-silent");
    });
  });

  describe("clearing", () => {
    it("the mic coming back clears it", () => {
      const broken = healthy({ micSoundAt: 0, engineSpokeAt: 0, audioFrameAt: 0 });
      expect(assessAudioHealth(broken)).toBe("mic-silent");
      expect(assessAudioHealth({ ...broken, micSoundAt: NOW - 100 })).toBe("ok");
    });

    it("audio arriving clears a missing reply", () => {
      const broken = healthy({ engineSpokeAt: NOW - 8000, audioFrameAt: 0 });
      expect(assessAudioHealth(broken)).toBe("no-audio");
      expect(assessAudioHealth({ ...broken, audioFrameAt: NOW - 100 })).toBe("ok");
    });

    it("a context that starts running again clears the blocked line", () => {
      const broken = healthy({ audioRunning: false });
      expect(assessAudioHealth(broken)).toBe("cannot-play");
      expect(assessAudioHealth({ ...broken, audioRunning: true })).toBe("ok");
    });
  });

  it("every fault has something to say, and it says what to check", () => {
    for (const message of Object.values(HEALTH_MESSAGE)) {
      expect(message.length).toBeGreaterThan(10);
    }
    expect(new Set(Object.values(HEALTH_MESSAGE)).size).toBe(Object.keys(HEALTH_MESSAGE).length);
  });
});
