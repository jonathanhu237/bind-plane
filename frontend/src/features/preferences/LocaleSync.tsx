import { useEffect } from "react";

import { i18n } from "@/i18n/i18n";
import { usePreferencesStore } from "@/stores/preferences";

export function LocaleSync() {
  const locale = usePreferencesStore((state) => state.locale);

  useEffect(() => {
    if (i18n.language !== locale) {
      void i18n.changeLanguage(locale);
    }
  }, [locale]);

  return null;
}
