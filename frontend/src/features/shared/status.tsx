import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { statusLabel } from "@/i18n/labels";
import { cn } from "@/lib/utils";

export function StatusBadge({ value }: { value: string }) {
  const { t } = useTranslation();
  const normalized = statusLabel(t, value);
  const isSuccess = value === "succeeded" || value === "ready";
  const isWarning =
    value === "needs_manual_confirmation" || value === "waiting_confirmation";
  const isDestructive =
    value === "failed" || value === "timeout" || value === "cancelled";

  return (
    <Badge
      className={cn(
        "capitalize",
        isSuccess &&
          "border-emerald-200 bg-emerald-50 text-emerald-700 hover:bg-emerald-50",
        isWarning &&
          "border-amber-200 bg-amber-50 text-amber-700 hover:bg-amber-50",
      )}
      variant={
        isDestructive
          ? "destructive"
          : isSuccess || isWarning
            ? "outline"
            : "secondary"
      }
    >
      {normalized}
    </Badge>
  );
}

export function EmptyState({ label }: { label: string }) {
  return (
    <div className="rounded-md border border-dashed bg-muted/30 px-4 py-8 text-center text-sm text-muted-foreground">
      {label}
    </div>
  );
}
