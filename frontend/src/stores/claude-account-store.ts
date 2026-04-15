import { create } from "zustand";
import {
  claudeLogin,
  claudeLoginCancel,
  claudeLoginCode,
  claudeLogout,
  getClaudeAccount,
} from "~/lib/claude-account-api";

interface ClaudeAccountState {
  loggedIn: boolean;
  email: string | null;
  orgName: string | null;
  subscriptionType: string | null;
  loading: boolean;
  switching: boolean;
  loginUrl: string | null;
  loginDialogOpen: boolean;
  error: string | null;
  submittingCode: boolean;

  fetchStatus: () => Promise<void>;
  switchAccount: () => Promise<void>;
  loginAccount: () => Promise<void>;
  cancelLogin: () => Promise<void>;
  submitCode: (code: string) => Promise<void>;
  closeLoginDialog: () => void;
}

const POLL_INTERVAL = 2000;
const POLL_TIMEOUT = 5 * 60 * 1000;

let pollAbortController: AbortController | null = null;

export const useClaudeAccountStore = create<ClaudeAccountState>((set, get) => ({
  loggedIn: false,
  email: null,
  orgName: null,
  subscriptionType: null,
  loading: true,
  switching: false,
  loginUrl: null,
  loginDialogOpen: false,
  error: null,
  submittingCode: false,

  fetchStatus: async () => {
    try {
      const account = await getClaudeAccount();
      set({
        loggedIn: account.loggedIn,
        email: account.email ?? null,
        orgName: account.orgName ?? null,
        subscriptionType: account.subscriptionType ?? null,
        loading: false,
      });
    } catch {
      set({ loading: false });
    }
  },

  switchAccount: async () => {
    set({ switching: true, loginDialogOpen: true, error: null });
    try {
      await claudeLogout();
      set({ loggedIn: false, email: null, orgName: null, subscriptionType: null });
      await get().loginAccount();
    } catch (err) {
      set({
        switching: false,
        loginUrl: null,
        error: err instanceof Error ? err.message : "Switch failed",
      });
    }
  },

  loginAccount: async () => {
    set({ switching: true, loginDialogOpen: true, error: null });
    try {
      const result = await claudeLogin();
      if (result.url) {
        window.open(result.url, "_blank");
      }
      set({ loginUrl: result.url ?? null });
      if (result.status === "already_logged_in") {
        await get().fetchStatus();
        set({ switching: false, loginUrl: null, loginDialogOpen: false });
        return;
      }
      pollAbortController = new AbortController();
      await pollUntilLoggedIn(set, pollAbortController.signal);
    } catch (err) {
      set({
        switching: false,
        loginUrl: null,
        error: err instanceof Error ? err.message : "Login failed",
      });
    }
  },

  cancelLogin: async () => {
    pollAbortController?.abort();
    pollAbortController = null;
    try {
      await claudeLoginCancel();
    } catch {
      // best effort
    }
    set({ switching: false, loginUrl: null, loginDialogOpen: false, error: null });
  },

  submitCode: async (code: string) => {
    set({ submittingCode: true, error: null });
    try {
      await claudeLoginCode(code);
    } catch (err) {
      set({
        submittingCode: false,
        error: err instanceof Error ? err.message : "Code submission failed",
      });
      return;
    }
    set({ submittingCode: false });

    // Cancel any existing poll (may have timed out) and start fresh
    pollAbortController?.abort();
    pollAbortController = new AbortController();
    set({ switching: true, error: null });
    await pollUntilLoggedIn(set, pollAbortController.signal);
  },

  closeLoginDialog: () => {
    // If login is in progress, cancel it
    if (get().switching) {
      get().cancelLogin();
    } else {
      set({ loginDialogOpen: false, error: null });
    }
  },
}));

type SetFn = (partial: Partial<ClaudeAccountState>) => void;

async function pollUntilLoggedIn(set: SetFn, signal: AbortSignal): Promise<void> {
  const deadline = Date.now() + POLL_TIMEOUT;

  return new Promise<void>((resolve) => {
    const check = async () => {
      if (signal.aborted) {
        resolve();
        return;
      }
      if (Date.now() > deadline) {
        set({ switching: false, loginUrl: null, error: "Login timed out" });
        claudeLoginCancel().catch((err) => console.error("claudeLoginCancel failed", err));
        resolve();
        return;
      }
      const account = await getClaudeAccount();
      if (account.loggedIn) {
        set({
          loggedIn: true,
          email: account.email ?? null,
          orgName: account.orgName ?? null,
          subscriptionType: account.subscriptionType ?? null,
          switching: false,
          loginUrl: null,
          loginDialogOpen: false,
          error: null,
        });
        resolve();
        return;
      }
      setTimeout(check, POLL_INTERVAL);
    };
    setTimeout(check, POLL_INTERVAL);
  });
}
