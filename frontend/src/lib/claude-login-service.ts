import {
  type ClaudeAccount,
  claudeLogin,
  claudeLoginCancel,
  claudeLoginCode,
  claudeLogout,
  getClaudeAccount,
} from "~/lib/claude-account-api";

const POLL_INTERVAL = 2000;
const POLL_TIMEOUT = 5 * 60 * 1000;

/** Transient flow flags the service patches onto the store during a login flow. */
export interface ClaudeLoginFlowPatch {
  switching?: boolean;
  submittingCode?: boolean;
  loginUrl?: string | null;
  loginDialogOpen?: boolean;
  error?: string | null;
}

/**
 * The store-facing surface the service writes through. Keeps the service free of
 * any knowledge of zustand — it only reports resolved account snapshots and
 * transient flow flags.
 */
export interface ClaudeLoginSink {
  /** Apply a resolved account snapshot (logged-in identity fields). */
  applyAccount: (account: ClaudeAccount) => void;
  /** Patch transient flow flags. */
  patch: (state: ClaudeLoginFlowPatch) => void;
}

export interface ClaudeLoginServiceOptions {
  /** Side effect for opening the OAuth URL. Injectable so the store stays DOM-free and tests stay headless. */
  openUrl?: (url: string) => void;
}

export interface ClaudeLoginService {
  login: () => Promise<void>;
  switchAccount: () => Promise<void>;
  submitCode: (code: string) => Promise<void>;
  cancel: () => Promise<void>;
  refreshStatus: () => Promise<void>;
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof Error ? err.message : fallback;
}

/**
 * Owns the OAuth login side effects: opening the browser window and the
 * setTimeout polling loop. The active poll's AbortController lives in this
 * closure (one per flow, captured locally inside each poll), so starting a new
 * flow cleanly supersedes the previous one — there is no shared module-level
 * controller that concurrent flows can stomp.
 */
export function createClaudeLoginService(
  sink: ClaudeLoginSink,
  options: ClaudeLoginServiceOptions = {},
): ClaudeLoginService {
  const openUrl =
    options.openUrl ??
    ((url: string) => {
      window.open(url, "_blank");
    });

  // The most recently started flow's controller. Replaced (after aborting the
  // prior one) whenever a new poll begins; each poll loop captures its OWN
  // signal locally, so an old loop can never observe or clear a newer flow's
  // controller.
  let active: AbortController | null = null;

  const poll = (): Promise<void> => {
    active?.abort();
    const controller = new AbortController();
    active = controller;
    const { signal } = controller;
    const deadline = Date.now() + POLL_TIMEOUT;

    return new Promise<void>((resolve) => {
      const check = async () => {
        if (signal.aborted) {
          resolve();
          return;
        }
        if (Date.now() > deadline) {
          sink.patch({ switching: false, loginUrl: null, error: "Login timed out" });
          claudeLoginCancel().catch((err) => console.error("claudeLoginCancel failed", err));
          resolve();
          return;
        }
        try {
          const account = await getClaudeAccount();
          if (account.loggedIn) {
            sink.applyAccount(account);
            sink.patch({
              switching: false,
              loginUrl: null,
              loginDialogOpen: false,
              error: null,
            });
            resolve();
            return;
          }
        } catch (err) {
          console.error("getClaudeAccount failed during login polling", err);
        }
        if (signal.aborted) {
          resolve();
          return;
        }
        setTimeout(check, POLL_INTERVAL);
      };
      setTimeout(check, POLL_INTERVAL);
    });
  };

  const login = async (): Promise<void> => {
    sink.patch({ switching: true, loginDialogOpen: true, error: null });
    try {
      const result = await claudeLogin();
      if (result.url) openUrl(result.url);
      sink.patch({ loginUrl: result.url ?? null });
      if (result.status === "already_logged_in") {
        await refreshStatus();
        sink.patch({ switching: false, loginUrl: null, loginDialogOpen: false });
        return;
      }
      await poll();
    } catch (err) {
      sink.patch({
        switching: false,
        loginUrl: null,
        error: errorMessage(err, "Login failed"),
      });
    }
  };

  const switchAccount = async (): Promise<void> => {
    sink.patch({ switching: true, loginDialogOpen: true, error: null });
    try {
      await claudeLogout();
      sink.applyAccount({ loggedIn: false });
      await login();
    } catch (err) {
      sink.patch({
        switching: false,
        loginUrl: null,
        error: errorMessage(err, "Switch failed"),
      });
    }
  };

  const submitCode = async (code: string): Promise<void> => {
    sink.patch({ submittingCode: true, error: null });
    try {
      await claudeLoginCode(code);
    } catch (err) {
      sink.patch({
        submittingCode: false,
        error: errorMessage(err, "Code submission failed"),
      });
      return;
    }
    sink.patch({ submittingCode: false, switching: true, error: null });
    await poll();
  };

  const cancel = async (): Promise<void> => {
    active?.abort();
    active = null;
    try {
      await claudeLoginCancel();
    } catch {
      // best effort
    }
    sink.patch({ switching: false, loginUrl: null, loginDialogOpen: false, error: null });
  };

  const refreshStatus = async (): Promise<void> => {
    const account = await getClaudeAccount();
    sink.applyAccount(account);
  };

  return { login, switchAccount, submitCode, cancel, refreshStatus };
}
