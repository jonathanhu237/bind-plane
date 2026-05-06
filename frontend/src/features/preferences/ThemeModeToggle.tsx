import { Monitor, Moon, Sun } from "lucide-react";
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
  isThemeMode,
  type ThemeMode,
  usePreferencesStore,
} from "@/stores/preferences";

const themeOptions: Array<{
  value: ThemeMode;
  labelKey: string;
  icon: typeof Sun;
}> = [
  { value: "light", labelKey: "preferences.light", icon: Sun },
  { value: "dark", labelKey: "preferences.dark", icon: Moon },
  { value: "system", labelKey: "preferences.system", icon: Monitor },
];

type ThemeModeToggleProps = {
  className?: string;
};

export function ThemeModeToggle({ className }: ThemeModeToggleProps) {
  const { t } = useTranslation();
  const themeMode = usePreferencesStore((state) => state.themeMode);
  const setThemeMode = usePreferencesStore((state) => state.setThemeMode);
  const selectedTheme =
    themeOptions.find((option) => option.value === themeMode) ??
    themeOptions[2];
  const SelectedIcon = selectedTheme.icon;

  function updateThemeMode(value: string) {
    if (isThemeMode(value)) {
      setThemeMode(value);
    }
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          aria-label={t("preferences.themeMode")}
          className={cn("shrink-0", className)}
          size="icon"
          title={t("preferences.themeMode")}
          variant="ghost"
        >
          <SelectedIcon size={16} />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuLabel>{t("preferences.theme")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuRadioGroup
          onValueChange={updateThemeMode}
          value={themeMode}
        >
          {themeOptions.map((option) => {
            const Icon = option.icon;
            return (
              <DropdownMenuRadioItem key={option.value} value={option.value}>
                <Icon />
                <span>{t(option.labelKey)}</span>
              </DropdownMenuRadioItem>
            );
          })}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
