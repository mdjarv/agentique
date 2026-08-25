import { describe, expect, it } from "vitest";
import { reduceTranscript, type TranscriptResultList } from "~/lib/speech-transcript";

/** Build a result list the way the engine hands one over: ordered, `[0]` best. */
function results(...entries: Array<[transcript: string, isFinal: boolean]>): TranscriptResultList {
  return entries.map(([transcript, isFinal]) => ({ 0: { transcript }, isFinal }));
}

const FINAL = true;
const INTERIM = false;

describe("reduceTranscript", () => {
  it("returns an empty string for an empty result list", () => {
    expect(reduceTranscript(results())).toBe("");
  });

  it("accumulates finals in order, one space between them", () => {
    expect(reduceTranscript(results(["hello", FINAL], ["there", FINAL], ["friend", FINAL]))).toBe(
      "hello there friend",
    );
  });

  it("normalizes the engine's inconsistent leading and trailing spaces", () => {
    expect(
      reduceTranscript(results(["hello", FINAL], [" there", FINAL], ["  friend ", FINAL])),
    ).toBe("hello there friend");
  });

  it("appends the trailing interim to the finals", () => {
    expect(reduceTranscript(results(["hello", FINAL], [" there", INTERIM]))).toBe("hello there");
  });

  it("replaces the interim across successive events rather than accumulating it", () => {
    expect(reduceTranscript(results(["hello", FINAL], ["the", INTERIM]))).toBe("hello the");
    expect(reduceTranscript(results(["hello", FINAL], ["there", INTERIM]))).toBe("hello there");
    expect(reduceTranscript(results(["hello", FINAL], ["there friend", INTERIM]))).toBe(
      "hello there friend",
    );
  });

  it("keeps only the last of several interims in one list", () => {
    expect(reduceTranscript(results(["the", INTERIM], ["there", INTERIM]))).toBe("there");
  });

  it("drops a superseded interim sitting beside the final that replaced it", () => {
    // What Chrome leaves behind across an internal speech-service restart: the
    // interim guess still in the list, immediately followed by its final.
    expect(reduceTranscript(results(["hello world", INTERIM], ["hello world", FINAL]))).toBe(
      "hello world",
    );
  });

  it("drops a superseded interim mid-list without losing the live one", () => {
    expect(
      reduceTranscript(
        results(["hello", FINAL], ["there", INTERIM], ["there", FINAL], ["friend", INTERIM]),
      ),
    ).toBe("hello there friend");
  });

  it("returns the interim alone when nothing has been finalized yet", () => {
    expect(reduceTranscript(results(["hello", INTERIM]))).toBe("hello");
  });

  it("skips results carrying no alternative", () => {
    const list: TranscriptResultList = [
      { 0: { transcript: "hello" }, isFinal: FINAL },
      { isFinal: FINAL },
      { 0: { transcript: "there" }, isFinal: FINAL },
    ];
    expect(reduceTranscript(list)).toBe("hello there");
  });

  it("ignores empty transcripts rather than emitting stray spaces", () => {
    expect(reduceTranscript(results(["hello", FINAL], ["", FINAL], [" ", INTERIM]))).toBe("hello");
  });
});
