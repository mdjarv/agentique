import { describe, expect, it } from "vitest";
import { dictationStatus } from "./dictation-status";

describe("dictationStatus", () => {
  it("walks the server route's phases in words", () => {
    expect(dictationStatus("server", "connecting").word).toBe("Connecting");
    expect(dictationStatus("server", "listening").sub).toBe("text appears when you pause");
    expect(dictationStatus("server", "hearing")).toMatchObject({
      mark: "wave",
      word: "Hearing you",
      sub: "via Gemini",
    });
    expect(dictationStatus("server", "writing")).toMatchObject({
      mark: "writing",
      stopHint: false,
    });
  });

  it("reads the browser route as listening, since its words stream", () => {
    for (const phase of [null, "hearing"] as const) {
      expect(dictationStatus("browser", phase)).toMatchObject({
        word: "Listening",
        sub: "via the browser",
      });
    }
  });
});
