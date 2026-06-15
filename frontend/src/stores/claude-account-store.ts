import { create } from "zustand";
import { getClaudeAccount } from "~/lib/claude-account-api";
import { createClaudeLoginService } from "~/lib/claude-login-service";

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

export const useClaudeAccountStore = create<ClaudeAccountState>((set, get) => {
  // The service owns all side effects (window.open, the polling loop, its own
  // abort controller). The store only holds flags/status and exposes a sink the
  // service writes resolved state through — keeping the store DOM-free.
  const service = createClaudeLoginService({
    applyAccount: (account) =>
      set({
        loggedIn: account.loggedIn,
        email: account.email ?? null,
        orgName: account.orgName ?? null,
        subscriptionType: account.subscriptionType ?? null,
      }),
    patch: (state) => set(state),
  });

  return {
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

    switchAccount: () => service.switchAccount(),

    loginAccount: () => service.login(),

    cancelLogin: () => service.cancel(),

    submitCode: (code: string) => service.submitCode(code),

    closeLoginDialog: () => {
      // If login is in progress, cancel it (aborts the poll); otherwise just close.
      if (get().switching) {
        service.cancel();
      } else {
        set({ loginDialogOpen: false, error: null });
      }
    },
  };
});
