import { Languages } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { cn } from "@/lib/utils";
import {
  isLocale,
  type Locale,
  usePreferencesStore,
} from "@/stores/preferences";

const localeOptions: Locale[] = ["zh-CN", "en-US"];

type LocaleSwitcherProps = {
  className?: string;
};

export function LocaleSwitcher({ className }: LocaleSwitcherProps) {
  const { t } = useTranslation();
  const locale = usePreferencesStore((state) => state.locale);
  const setLocale = usePreferencesStore((state) => state.setLocale);

  function updateLocale(value: string) {
    if (isLocale(value)) {
      setLocale(value);
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t("preferences.languageLabel")}
          className={cn("shrink-0", className)}
          size="icon"
          title={t("preferences.languageLabel")}
          variant="ghost"
        >
          <Languages size={16} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t("preferences.language")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup onValueChange={updateLocale} value={locale}>
          {localeOptions.map((option) => (
            <DropdownMenuRadioItem key={option} value={option}>
              {t(
                option === "zh-CN" ? "preferences.zhCN" : "preferences.enUS",
              )}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
