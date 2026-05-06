import { zodResolver } from "@hookform/resolvers/zod";
import { KeyRound, Shield } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { useNavigate } from "react-router-dom";
import { z } from "zod";

import { apiRequest } from "@/api/client";
import { Alert } from "@/components/ui/alert";
import { InputField } from "@/components/forms/fields";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Form } from "@/components/ui/form";
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
      const response = await apiRequest<{ access_token: string }>(
        "/auth/login",
        null,
        {
          method: "POST",
          body: JSON.stringify(values),
        },
      );
      setToken(response.access_token);
      navigate("/release", { replace: true });
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    }
  }

  return (
    <main className="flex min-h-svh w-full items-center justify-center bg-muted/40 p-6 md:p-10">
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center justify-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Shield size={20} />
          </div>
          <div className="text-sm font-semibold">Bind Plane</div>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>Sign in</CardTitle>
            <CardDescription>Use your Bind Plane account.</CardDescription>
          </CardHeader>
          <CardContent>
            <Form {...form}>
              <form className="grid gap-4" onSubmit={form.handleSubmit(submit)}>
                <InputField
                  autoComplete="username"
                  control={form.control}
                  label="Username"
                  name="username"
                />
                <InputField
                  autoComplete="current-password"
                  control={form.control}
                  label="Password"
                  name="password"
                  type="password"
                />
                {error ? <Alert variant="destructive">{error}</Alert> : null}
                <Button disabled={form.formState.isSubmitting} type="submit">
                  <KeyRound size={16} />
                  {form.formState.isSubmitting ? "Signing in" : "Sign in"}
                </Button>
              </form>
            </Form>
          </CardContent>
        </Card>
      </div>
    </main>
  );
}
