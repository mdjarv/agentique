import { create } from "zustand";
import {
  claudeLogin,
  claudeLoginCancel,
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

  fetchStatus: () => Promise<void>;
  switchAccount: () => Promise<void>;
  loginAccount: () => Promise<void>;
  cancelLogin: () => Promise<void>;
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
    set({ switching: true });
    try {
      await claudeLogout();
      set({ loggedIn: false, email: null, orgName: null, subscriptionType: null });
      await get().loginAccount();
    } catch {
      set({ switching: false, loginUrl: null });
    }
  },

  loginAccount: async () => {
    set({ switching: true });
    try {
      const result = await claudeLogin();
      if (result.url) {
        window.open(result.url, "_blank");
      }
      set({ loginUrl: result.url ?? null });
      if (result.status === "already_logged_in") {
        await get().fetchStatus();
        set({ switching: false, loginUrl: null });
        return;
      }
      pollAbortController = new AbortController();
      await pollUntilLoggedIn(set, pollAbortController.signal);
    } catch {
      set({ switching: false, loginUrl: null });
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
    set({ switching: false, loginUrl: null });
  },
}));

type SetFn = (partial: Partial<ClaudeAccountState>) => void;

async function pollUntilLoggedIn(set: SetFn, signal: AbortSignal): Promise<void> {
  const deadline = Date.now() + POLL_TIMEOUT;

  return new Promise<void>((resolve) => {
    const check = async () => {
      if (signal.aborted || Date.now() > deadline) {
        set({ switching: false, loginUrl: null });
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
        });
        resolve();
        return;
      }
      setTimeout(check, POLL_INTERVAL);
    };
    setTimeout(check, POLL_INTERVAL);
  });
}
