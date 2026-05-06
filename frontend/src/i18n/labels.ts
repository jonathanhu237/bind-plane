import type { TFunction } from "i18next";

import type { ReleaseReason } from "@/api/types";

export function reasonLabel(t: TFunction, reason: ReleaseReason) {
  return t(`reason.${reason}`);
}

export function statusLabel(t: TFunction, value: string) {
  return t(`status.${value}`, {
    defaultValue: value.replace(/_/g, " "),
  });
}

export function roleLabel(t: TFunction, value: string) {
  return t(`role.${value}`, { defaultValue: value });
}

export function activeLabel(t: TFunction, active: boolean) {
  return active ? t("common.active") : t("common.inactive");
}

export function booleanLabel(t: TFunction, value: boolean) {
  return value ? t("common.yes") : t("common.no");
}
