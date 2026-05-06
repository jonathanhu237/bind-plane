import i18n from "i18next";
import { initReactI18next } from "react-i18next";

import { resources } from "@/i18n/resources";
import { defaultLocale } from "@/stores/preferences";

void i18n.use(initReactI18next).init({
  resources,
  lng: defaultLocale,
  fallbackLng: "en-US",
  interpolation: {
    escapeValue: false,
  },
});

export { i18n };
