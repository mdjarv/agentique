import { create } from "zustand";
import { reconnectWebSocket } from "~/hooks/useWebSocket";
import type { AuthUser } from "~/lib/auth-api";
import { getAuthStatus } from "~/lib/auth-api";

interface AuthState {
  authEnabled: boolean;
  authenticated: boolean;
  user: AuthUser | null;
  userCount: number;
  loading: boolean;
  checkAuth: () => Promise<void>;
  setAuthenticated: (user: AuthUser) => void;
  clearAuth: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  authEnabled: false,
  authenticated: false,
  user: null,
  userCount: 0,
  loading: true,

  checkAuth: async () => {
    try {
      const status = await getAuthStatus();
      set({
        authEnabled: status.authEnabled,
        authenticated: status.authenticated,
        user: status.user ?? null,
        userCount: status.userCount,
        loading: false,
      });
    } catch {
      set({ loading: false });
    }
  },

  setAuthenticated: (user) => {
    set({ authenticated: true, user });
    reconnectWebSocket();
  },
  clearAuth: () => set({ authenticated: false, user: null }),
}));
