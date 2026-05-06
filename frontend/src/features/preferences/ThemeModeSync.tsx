import { useEffect } from "react";

import {
  type ThemeMode,
  usePreferencesStore,
} from "@/stores/preferences";

const COLOR_SCHEME_QUERY = "(prefers-color-scheme: dark)";

function applyThemeMode(themeMode: ThemeMode, systemDark: boolean) {
  if (typeof document === "undefined") {
    return;
  }
  const isDark = themeMode === "dark" || (themeMode === "system" && systemDark);
  document.documentElement.classList.toggle("dark", isDark);
  document.documentElement.style.colorScheme = isDark ? "dark" : "light";
}

export function ThemeModeSync() {
  const themeMode = usePreferencesStore((state) => state.themeMode);

  useEffect(() => {
    if (typeof window === "undefined" || typeof document === "undefined") {
      return undefined;
    }

    if (!("matchMedia" in window)) {
      applyThemeMode(themeMode, false);
      return undefined;
    }

    const mediaQuery = window.matchMedia(COLOR_SCHEME_QUERY);
    applyThemeMode(themeMode, mediaQuery.matches);

    if (themeMode !== "system") {
      return undefined;
    }

    const handleChange = (event: MediaQueryListEvent) => {
      applyThemeMode("system", event.matches);
    };

    mediaQuery.addEventListener?.("change", handleChange);
    mediaQuery.addListener?.(handleChange);

    return () => {
      mediaQuery.removeEventListener?.("change", handleChange);
      mediaQuery.removeListener?.(handleChange);
    };
  }, [themeMode]);

  return null;
}
