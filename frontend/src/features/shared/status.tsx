import { Badge } from "@/components/ui/badge";

export function StatusBadge({ value }: { value: string }) {
  const normalized = value.replace(/_/g, " ");
  const variant =
    value === "succeeded" || value === "ready"
      ? "success"
      : value === "failed" || value === "timeout" || value === "cancelled"
        ? "destructive"
        : value === "needs_manual_confirmation" || value === "waiting_confirmation"
          ? "warning"
          : "secondary";

  return <Badge variant={variant}>{normalized}</Badge>;
}

export function EmptyState({ label }: { label: string }) {
  return (
    <div className="rounded-md border border-dashed bg-muted/30 px-4 py-8 text-center text-sm text-muted-foreground">
      {label}
    </div>
  );
}
