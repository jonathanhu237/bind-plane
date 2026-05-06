import { zodResolver } from "@hookform/resolvers/zod";
import { KeyRound, Shield } from "lucide-react";
import { useMemo, useState } from "react";
import { useForm } from "react-hook-form";
import { useTranslation } from "react-i18next";
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
import { LocaleSwitcher } from "@/features/preferences/LocaleSwitcher";
import { ThemeModeToggle } from "@/features/preferences/ThemeModeToggle";
import { useAuthStore } from "@/stores/auth";

function createLoginSchema(t: (key: string) => string) {
  return z.object({
    username: z.string().min(1, t("validation.usernameRequired")),
    password: z.string().min(1, t("validation.passwordRequired")),
  });
}

type LoginValues = z.infer<ReturnType<typeof createLoginSchema>>;

export function LoginPage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const setToken = useAuthStore((state) => state.setToken);
  const [error, setError] = useState<string | null>(null);
  const loginSchema = useMemo(() => createLoginSchema(t), [t]);
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
      setError(err instanceof Error ? err.message : t("auth.loginFailed"));
    }
  }

  return (
    <main className="relative flex min-h-svh w-full items-center justify-center bg-muted/40 p-6 md:p-10">
      <div className="absolute right-4 top-4 flex items-center gap-1">
        <LocaleSwitcher />
        <ThemeModeToggle />
      </div>
      <div className="flex w-full max-w-sm flex-col gap-6">
        <div className="flex items-center justify-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Shield size={20} />
          </div>
          <div className="text-sm font-semibold">{t("app.name")}</div>
        </div>
        <Card>
          <CardHeader>
            <CardTitle>{t("auth.signIn")}</CardTitle>
            <CardDescription>{t("auth.accountHint")}</CardDescription>
          </CardHeader>
          <CardContent>
            <Form {...form}>
              <form className="grid gap-4" onSubmit={form.handleSubmit(submit)}>
                <InputField
                  autoComplete="username"
                  control={form.control}
                  label={t("auth.username")}
                  name="username"
                />
                <InputField
                  autoComplete="current-password"
                  control={form.control}
                  label={t("auth.password")}
                  name="password"
                  type="password"
                />
                {error ? <Alert variant="destructive">{error}</Alert> : null}
                <Button disabled={form.formState.isSubmitting} type="submit">
                  <KeyRound size={16} />
                  {form.formState.isSubmitting
                    ? t("auth.signingIn")
                    : t("auth.signIn")}
                </Button>
              </form>
            </Form>
          </CardContent>
        </Card>
      </div>
    </main>
  );
}
