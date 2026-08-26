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
export function joinTranscript(left: string, right: string): string {
  const head = left.trimEnd();
  const tail = right.trim();
  if (!head) return tail;
  if (!tail) return head;
  return `${head} ${tail}`;
}

/** What a result list holds, kept apart because the two have different lifetimes. */
export interface TranscriptParts {
  /**
   * The words the engine has committed. They survive the end of a recognition
   * session: the engine will not offer them again.
   */
  readonly finals: string;
  /**
   * The live guess at the words being spoken right now. It is not owned by
   * anything yet — if the session ends here, the audio behind it is re-offered
   * to whatever session comes next, so this must never be committed.
   */
  readonly interim: string;
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
  const { finals, interim } = reduceTranscriptParts(results);
  return joinTranscript(finals, interim);
}

/**
 * The same reduction, with the committed half kept apart from the guess.
 *
 * `useSpeechRecognition` needs the split because a dictation span outlives the
 * recognition sessions the browser slices it into, and only `finals` may cross
 * that seam. See the hook for what the browsers actually do.
 */
export function reduceTranscriptParts(results: TranscriptResultList): TranscriptParts {
  let finals = "";
  let interim = "";

  for (let i = 0; i < results.length; i++) {
    const result = results[i];
    const alt = result?.[0];
    if (!result || !alt) continue;

    if (result.isFinal) {
      finals = joinTranscript(finals, alt.transcript);
      // This final supersedes whatever was being guessed before it.
      interim = "";
      continue;
    }

    // Interims replace each other; they never accumulate.
    interim = alt.transcript;
  }

  return { finals, interim: interim.trim() };
}
