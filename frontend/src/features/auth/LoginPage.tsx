import { zodResolver } from "@hookform/resolvers/zod";
import { KeyRound, Shield } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate } from "react-router-dom";
import { z } from "zod";

import { apiRequest } from "@/api/client";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { FormField } from "@/components/ui/form";
import { Input } from "@/components/ui/input";
import { useAuthStore } from "@/stores/auth";

const loginSchema = z.object({
  username: z.string().min(1, "Username is required"),
  password: z.string().min(1, "Password is required"),
});

type LoginValues = z.infer<typeof loginSchema>;

export function LoginPage() {
  const navigate = useNavigate();
  const setToken = useAuthStore((state) => state.setToken);
  const [error, setError] = useState<string | null>(null);
  const form = useForm<LoginValues>({
    resolver: zodResolver(loginSchema),
    defaultValues: { username: "", password: "" },
  });

  async function submit(values: LoginValues) {
    setError(null);
    try {
      const response = await apiRequest<{ access_token: string }>("/auth/login", null, {
        method: "POST",
        body: JSON.stringify(values),
      });
      setToken(response.access_token);
      navigate("/release", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-100 px-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="flex items-center gap-3">
            <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary text-primary-foreground">
              <Shield size={20} />
            </div>
            <CardTitle>Bind Plane</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <form className="grid gap-4" onSubmit={form.handleSubmit(submit)}>
            <FormField label="Username" error={form.formState.errors.username}>
              <Input autoComplete="username" {...form.register("username")} />
            </FormField>
            <FormField label="Password" error={form.formState.errors.password}>
              <Input autoComplete="current-password" type="password" {...form.register("password")} />
            </FormField>
            {error ? <Alert>{error}</Alert> : null}
            <Button disabled={form.formState.isSubmitting} type="submit">
              <KeyRound size={16} />
              {form.formState.isSubmitting ? "Signing in" : "Sign in"}
            </Button>
          </form>
        </CardContent>
      </Card>
    </main>
  );
}
