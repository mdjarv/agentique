import { describe, expect, it } from "vitest";
import { parseServerMessage } from "~/lib/voice/protocol";

describe("parseServerMessage", () => {
  it("accepts an activity frame, label and all", () => {
    expect(
      parseServerMessage('{"type":"activity","label":"Summarizing Live Voice Dialog"}'),
    ).toEqual({ type: "activity", label: "Summarizing Live Voice Dialog" });
  });

  it("accepts an activity frame with no label — the clearing form", () => {
    expect(parseServerMessage('{"type":"activity"}')).toEqual({ type: "activity" });
  });

  it("accepts a summary frame", () => {
    expect(parseServerMessage('{"type":"summary","sessionId":"s1","headline":"done"}')).toEqual({
      type: "summary",
      sessionId: "s1",
      headline: "done",
    });
  });

  it("drops a control type this build does not know, rather than throwing", () => {
    expect(parseServerMessage('{"type":"someday"}')).toBeNull();
  });

  it("drops anything that is not a JSON object", () => {
    expect(parseServerMessage("not json")).toBeNull();
    expect(parseServerMessage("null")).toBeNull();
    expect(parseServerMessage("[]")).toBeNull();
  });
});
