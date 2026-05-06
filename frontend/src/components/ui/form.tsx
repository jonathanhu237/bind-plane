import React from "react";
import type { FieldError } from "react-hook-form";

import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

export function FormField({
  label,
  error,
  children,
  className,
}: {
  label: string;
  error?: FieldError | string;
  children: React.ReactNode;
  className?: string;
}) {
  const message = typeof error === "string" ? error : error?.message;
  const generatedId = React.useId();
  const child = React.Children.only(children);
  const childId =
    React.isValidElement<{ id?: string }>(child) && child.props.id ? child.props.id : generatedId;
  const control = React.isValidElement<{ id?: string; "aria-invalid"?: boolean }>(child)
    ? React.cloneElement(child, {
        id: childId,
        "aria-invalid": Boolean(message),
      })
    : child;
  return (
    <div className={cn("grid gap-2", className)}>
      <Label htmlFor={childId}>{label}</Label>
      {control}
      {message ? <p className="text-xs text-destructive">{message}</p> : null}
    </div>
  );
}
