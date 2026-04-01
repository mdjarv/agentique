import { create } from "zustand";
import { claudeLogin, claudeLogout, getClaudeAccount } from "~/lib/claude-account-api";

interface ClaudeAccountState {
  loggedIn: boolean;
  email: string | null;
  orgName: string | null;
  subscriptionType: string | null;
  loading: boolean;
  switching: boolean;

  fetchStatus: () => Promise<void>;
  switchAccount: () => Promise<void>;
  loginAccount: () => Promise<void>;
}

const POLL_INTERVAL = 2000;
const POLL_TIMEOUT = 5 * 60 * 1000;

export const useClaudeAccountStore = create<ClaudeAccountState>((set, get) => ({
  loggedIn: false,
  email: null,
  orgName: null,
  subscriptionType: null,
  loading: true,
  switching: false,

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
      set({ switching: false });
    }
  },

  loginAccount: async () => {
    set({ switching: true });
    try {
      const result = await claudeLogin();
      if (result.url) {
        window.open(result.url, "_blank");
      }
      if (result.status === "already_logged_in") {
        await get().fetchStatus();
        set({ switching: false });
        return;
      }
      await pollUntilLoggedIn(set);
    } catch {
      set({ switching: false });
    }
  },
}));

type SetFn = (partial: Partial<ClaudeAccountState>) => void;

async function pollUntilLoggedIn(set: SetFn): Promise<void> {
  const deadline = Date.now() + POLL_TIMEOUT;

  return new Promise<void>((resolve) => {
    const check = async () => {
      if (Date.now() > deadline) {
        set({ switching: false });
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
        });
        resolve();
        return;
      }
      setTimeout(check, POLL_INTERVAL);
    };
    setTimeout(check, POLL_INTERVAL);
  });
}
