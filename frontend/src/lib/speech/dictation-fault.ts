/**
 * Why dictation cannot work here, as one closed set with the words for each.
 *
 * The Web Speech API is present in browsers that cannot use it, and it fails
 * without saying so: Brave ships Chromium's recognizer but blocks the Google
 * service behind it, so every session opens the microphone and ends in a
 * `network` error. Safari refuses with `service-not-allowed` when macOS
 * Dictation is switched off. Left to the restart budget, both looked like a mic
 * that lit up for a few seconds and went dark on its own.
 *
 * Two of these are knowable before a click (`probeDictationFault`); the rest
 * only from the error the first attempt ends in (`faultForError`).
 */

export type DictationFault =
  | "browser-blocked"
  | "mic-denied"
  | "service-disabled"
  | "service-unreachable"
  | "no-microphone"
  | "language-unsupported";

export const DICTATION_FAULT_COPY: Record<DictationFault, { title: string; detail: string }> = {
  "browser-blocked": {
    title: "Dictation isn't available in Brave",
    detail: "Brave blocks the speech service this browser API relies on. Use Chrome or Safari.",
  },
  "mic-denied": {
    title: "Microphone access is blocked",
    detail:
      "Allow the microphone (and, in Safari, speech recognition) for this site in the browser's settings, then try again.",
  },
  "service-disabled": {
    title: "The browser's speech service is turned off",
    detail: "In Safari on a Mac, turn on Dictation in System Settings → Keyboard, then try again.",
  },
  "service-unreachable": {
    title: "Couldn't reach the speech service",
    detail:
      "The browser sends dictation to its vendor's service. Check the connection; some browsers (Brave, for one) block it.",
  },
  "no-microphone": {
    title: "No microphone found",
    detail: "Connect a microphone or pick one in the system's sound settings.",
  },
  "language-unsupported": {
    title: "This language isn't supported for dictation",
    detail: "Change the browser's language, or dictate in a supported one.",
  },
};

/**
 * The fault a recognition error names, or null for one that is not a fault.
 *
 * `network` is a fault only before the span has heard anything. Once results
 * have arrived the service demonstrably works, and a mid-dictation `network` is
 * the transient kind Android throws routinely — that stays the restart budget's
 * to ride out. Before any result it is what Brave's block looks like, and three
 * retries only hold the microphone for longer before the same end.
 */
export function faultForError(code: string, heardAnything: boolean): DictationFault | null {
  switch (code) {
    case "not-allowed":
      return "mic-denied";
    case "service-not-allowed":
      return "service-disabled";
    case "audio-capture":
      return "no-microphone";
    case "language-not-supported":
      return "language-unsupported";
    case "network":
      return heardAnything ? null : "service-unreachable";
    default:
      return null;
  }
}

/** The parts of `navigator` the probe reads, so tests need no browser. */
export interface ProbeNavigator {
  brave?: { isBrave?: () => Promise<boolean> };
  permissions?: { query: (desc: { name: PermissionName }) => Promise<PermissionStatus> };
}

/**
 * The faults knowable before a click. Each check fails open: a probe that
 * throws or is missing says nothing, because refusing dictation on a guess is
 * worse than letting the attempt report what is actually wrong.
 *
 * `onChange` fires when the microphone permission changes later, so granting it
 * in site settings clears the mark without a reload.
 */
export async function probeDictationFault(
  nav: ProbeNavigator,
  onChange?: (fault: DictationFault | null) => void,
): Promise<DictationFault | null> {
  if (await isBrave(nav)) return "browser-blocked";

  const status = await queryMicrophone(nav);
  if (!status) return null;
  if (onChange) {
    status.onchange = () => onChange(status.state === "denied" ? "mic-denied" : null);
  }
  return status.state === "denied" ? "mic-denied" : null;
}

async function isBrave(nav: ProbeNavigator): Promise<boolean> {
  try {
    return (await nav.brave?.isBrave?.()) === true;
  } catch {
    return false;
  }
}

async function queryMicrophone(nav: ProbeNavigator): Promise<PermissionStatus | null> {
  try {
    return (await nav.permissions?.query({ name: "microphone" as PermissionName })) ?? null;
  } catch {
    // Firefox rejects "microphone" as a permission name.
    return null;
  }
}
