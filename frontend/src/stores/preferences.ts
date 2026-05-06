import { create } from "zustand";

export const THEME_STORAGE_KEY = "bind-plane-theme-mode";
export const LOCALE_STORAGE_KEY = "bind-plane-locale";

export type ThemeMode = "light" | "dark" | "system";
export type Locale = "zh-CN" | "en-US";

export const defaultLocale: Locale = "zh-CN";

export function isThemeMode(value: string | null): value is ThemeMode {
  return value === "light" || value === "dark" || value === "system";
}

export function isLocale(value: string | null): value is Locale {
  return value === "zh-CN" || value === "en-US";
}

function readStoredThemeMode(): ThemeMode {
  try {
    const value = globalThis.localStorage?.getItem(THEME_STORAGE_KEY) ?? null;
    return isThemeMode(value) ? value : "system";
  } catch {
    return "system";
  }
}

function readStoredLocale(): Locale {
  try {
    const value = globalThis.localStorage?.getItem(LOCALE_STORAGE_KEY) ?? null;
    return isLocale(value) ? value : defaultLocale;
  } catch {
    return defaultLocale;
  }
}

function writeStoredThemeMode(themeMode: ThemeMode) {
  try {
    globalThis.localStorage?.setItem(THEME_STORAGE_KEY, themeMode);
  } catch {
    // Storage can be unavailable in constrained browser/test environments.
  }
}

function writeStoredLocale(locale: Locale) {
  try {
    globalThis.localStorage?.setItem(LOCALE_STORAGE_KEY, locale);
  } catch {
    // Storage can be unavailable in constrained browser/test environments.
  }
}

type PreferencesState = {
  themeMode: ThemeMode;
  locale: Locale;
  setThemeMode: (themeMode: ThemeMode) => void;
  setLocale: (locale: Locale) => void;
};

export const usePreferencesStore = create<PreferencesState>((set) => ({
  themeMode: readStoredThemeMode(),
  locale: readStoredLocale(),
  setThemeMode: (themeMode) => {
    writeStoredThemeMode(themeMode);
    set({ themeMode });
  },
  setLocale: (locale) => {
    writeStoredLocale(locale);
    set({ locale });
  },
}));
