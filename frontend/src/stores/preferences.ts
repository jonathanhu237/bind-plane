import { create } from "zustand";

export const THEME_STORAGE_KEY = "bind-plane-theme-mode";

export type ThemeMode = "light" | "dark" | "system";

export function isThemeMode(value: string | null): value is ThemeMode {
  return value === "light" || value === "dark" || value === "system";
}

function readStoredThemeMode(): ThemeMode {
  try {
    const value = globalThis.localStorage?.getItem(THEME_STORAGE_KEY) ?? null;
    return isThemeMode(value) ? value : "system";
  } catch {
    return "system";
  }
}

function writeStoredThemeMode(themeMode: ThemeMode) {
  try {
    globalThis.localStorage?.setItem(THEME_STORAGE_KEY, themeMode);
  } catch {
    // Storage can be unavailable in constrained browser/test environments.
  }
}

type PreferencesState = {
  themeMode: ThemeMode;
  setThemeMode: (themeMode: ThemeMode) => void;
};

export const usePreferencesStore = create<PreferencesState>((set) => ({
  themeMode: readStoredThemeMode(),
  setThemeMode: (themeMode) => {
    writeStoredThemeMode(themeMode);
    set({ themeMode });
  },
}));
