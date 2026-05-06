import { create } from "zustand";

const AUTH_STORAGE_KEY = "bind-plane-auth-token";

function readStoredToken(): string | null {
  try {
    return globalThis.localStorage?.getItem(AUTH_STORAGE_KEY) ?? null;
  } catch {
    return null;
  }
}

function writeStoredToken(token: string | null) {
  try {
    if (!globalThis.localStorage) {
      return;
    }
    if (token) {
      globalThis.localStorage.setItem(AUTH_STORAGE_KEY, token);
    } else {
      globalThis.localStorage.removeItem(AUTH_STORAGE_KEY);
    }
  } catch {
    // Storage can be unavailable in constrained browser/test environments.
  }
}

type AuthState = {
  token: string | null;
  setToken: (token: string) => void;
  clearToken: () => void;
};

export const useAuthStore = create<AuthState>((set) => ({
  token: readStoredToken(),
  setToken: (token) => {
    writeStoredToken(token);
    set({ token });
  },
  clearToken: () => {
    writeStoredToken(null);
    set({ token: null });
  },
}));
