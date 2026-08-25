/**
 * Reduction of a Web Speech result list to the text it currently represents.
 *
 * Kept out of `useSpeechRecognition` because it is pure: the hook owns the
 * recognizer's lifecycle, this owns what its output means.
 */

/** One recognition result. `[0]` is the engine's best alternative. */
export interface TranscriptResult {
  readonly isFinal: boolean;
  readonly [index: number]: { readonly transcript: string } | undefined;
}

/** Structural stand-in for `SpeechRecognitionResultList`, so tests need no DOM. */
export interface TranscriptResultList {
  readonly length: number;
  readonly [index: number]: TranscriptResult | undefined;
}

/**
 * Join two parts with exactly one space, introducing no leading or trailing
 * whitespace. Chrome supplies its own leading spaces inconsistently — sometimes
 * on a final, sometimes not — so normalizing here is what keeps the composer
 * from rendering doubled spaces mid-sentence.
 */
function join(left: string, right: string): string {
  const head = left.trimEnd();
  const tail = right.trim();
  if (!head) return tail;
  if (!tail) return head;
  return `${head} ${tail}`;
}

/**
 * The transcript a result list currently represents.
 *
 * Recognition runs with `continuous` and `interimResults`, so every `onresult`
 * event carries the *whole* list: the results finalized so far plus, usually,
 * one in-flight interim at the end. The list is rebuilt from scratch on each
 * event — there is no accumulator across events.
 *
 * **Only finals accumulate.** An interim is a live guess at the words being
 * spoken right now, and the final that lands next *replaces* those words rather
 * than continuing them. Adding interims to the running text therefore prints the
 * same phrase twice. Chrome makes this unmissable: it restarts its internal
 * speech service every few seconds of continuous speech, and can leave the
 * superseded interim entry sitting in the list next to the final that replaced
 * it. That superseded entry always precedes its final (the list is ordered by
 * utterance, and Chrome appends the final after the restart), which is why a
 * final clears any interim seen before it and only a trailing interim survives.
 *
 * Break that invariant — accumulate interims, or keep one across a final — and
 * dictation duplicates phrases again.
 */
export function reduceTranscript(results: TranscriptResultList): string {
  let finalText = "";
  let interim = "";

  for (let i = 0; i < results.length; i++) {
    const result = results[i];
    const alt = result?.[0];
    if (!result || !alt) continue;

    if (result.isFinal) {
      finalText = join(finalText, alt.transcript);
      // This final supersedes whatever was being guessed before it.
      interim = "";
      continue;
    }

    // Interims replace each other; they never accumulate.
    interim = alt.transcript;
  }

  return join(finalText, interim);
}
