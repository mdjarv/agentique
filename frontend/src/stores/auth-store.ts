import { create } from "zustand";
import { reconnectWebSocket } from "~/hooks/useWebSocket";
import type { AuthUser } from "~/lib/auth-api";
import { getAuthStatus, updateUserPreferences } from "~/lib/auth-api";

interface AuthState {
  authEnabled: boolean;
  authenticated: boolean;
  user: AuthUser | null;
  userCount: number;
  credentialCount: number;
  loading: boolean;
  checkAuth: () => Promise<void>;
  setAuthenticated: (user: AuthUser) => void;
  clearAuth: () => void;
  setSidebarFocusMode: (enabled: boolean) => Promise<void>;
}

export const useAuthStore = create<AuthState>((set, get) => ({
  authEnabled: false,
  authenticated: false,
  user: null,
  userCount: 0,
  credentialCount: 0,
  loading: true,

  checkAuth: async () => {
    try {
      const status = await getAuthStatus();
      set({
        authEnabled: status.authEnabled,
        authenticated: status.authenticated,
        user: status.user ?? null,
        userCount: status.userCount,
        credentialCount: status.credentialCount,
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

  setSidebarFocusMode: async (enabled) => {
    const user = get().user;
    if (!user) return;
    set({ user: { ...user, sidebarFocusMode: enabled } });
    try {
      await updateUserPreferences({ sidebarFocusMode: enabled });
    } catch (err) {
      console.error("Failed to persist sidebar focus mode", err);
      set({ user: { ...user } });
    }
  },
}));
